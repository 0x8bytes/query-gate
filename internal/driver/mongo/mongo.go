// Package mongo 是 driver.Driver 的 MongoDB 实现。
// 本期 query 仅支持 JSON 形态 {"collection","pipeline"|"find"},不解析 mongosh 原生语句(TODO)。
package mongo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0x8bytes/query-gate/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Driver 是 MongoDB 实例的 driver.Driver 实现。
type Driver struct {
	name   string
	desc   string
	client *mongo.Client
	dbName string
}

// New 连接 MongoDB。dsn 形如 mongodb://user:pass@host:27017/dbname
func New(name, dsn, desc string) (*Driver, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// v1 API: options.Client().ApplyURI(dsn) + mongo.Connect(ctx, opts)
	opts := options.Client().ApplyURI(dsn)
	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("connect mongo %s: %w", name, err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("ping mongo %s: %w", name, err)
	}
	dbName := databaseFromURI(dsn)
	if dbName == "" {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongo %s: dsn must include database name", name)
	}
	return &Driver{name: name, desc: desc, client: client, dbName: dbName}, nil
}

// Info 返回实例元数据（不含 DSN）。
func (d *Driver) Info() model.DatabaseInfo {
	return model.DatabaseInfo{Name: d.name, Driver: "mongodb", Description: d.desc}
}

// Tables 返回 collection 列表（comment 恒为空）。
func (d *Driver) Tables(ctx context.Context) ([]model.TableInfo, error) {
	names, err := d.client.Database(d.dbName).ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	out := make([]model.TableInfo, 0, len(names))
	for _, n := range names {
		out = append(out, model.TableInfo{Name: n})
	}
	return out, nil
}

// Schema 采样推断字段：取 collection 前 20 条文档，归并 key 与类型。
// 空 collection 放入 notFound。
func (d *Driver) Schema(ctx context.Context, collections []string) (map[string]string, []string, error) {
	ddl := map[string]string{}
	var notFound []string
	for _, c := range collections {
		coll := d.client.Database(d.dbName).Collection(c)
		cur, err := coll.Find(ctx, bson.D{}, options.Find().SetLimit(20))
		if err != nil {
			return nil, nil, err
		}
		fields := map[string]string{}
		for cur.Next(ctx) {
			var doc bson.M
			if err := cur.Decode(&doc); err != nil {
				cur.Close(ctx)
				return nil, nil, err
			}
			for k, v := range doc {
				if _, seen := fields[k]; !seen {
					fields[k] = fmt.Sprintf("%T", v)
				}
			}
		}
		cur.Close(ctx)
		if len(fields) == 0 {
			notFound = append(notFound, c)
			continue
		}
		ddl[c] = formatFields(c, fields)
	}
	return ddl, notFound, nil
}

// mongoQuery 是 JSON 查询格式的结构体。
type mongoQuery struct {
	Collection string   `json:"collection"`
	Pipeline   []bson.M `json:"pipeline"`
	Find       bson.M   `json:"find"`
}

// parseQuery 解析 JSON 查询字符串；非 JSON 或缺 collection 均报错。
// mongosh 原生语法（如 db.users.find()）本期不支持，返回友好错误。
func parseQuery(query string) (*mongoQuery, error) {
	var q mongoQuery
	if err := json.Unmarshal([]byte(query), &q); err != nil {
		return nil, fmt.Errorf("mongo query must be JSON {\"collection\",\"pipeline\"|\"find\"}: %v", err)
	}
	if q.Collection == "" {
		return nil, fmt.Errorf("mongo query: collection is required")
	}
	return &q, nil
}

// Query 执行 pipeline 或 find，返回结果（每行一个 JSON 文档，列名为 "document"）。
func (d *Driver) Query(ctx context.Context, query string, limit, maxRows int) (*model.QueryResult, error) {
	q, err := parseQuery(query)
	if err != nil {
		return nil, err
	}
	rowCap := maxRows
	if limit > 0 && limit < rowCap {
		rowCap = limit
	}
	if rowCap <= 0 {
		rowCap = 10000 // 兜底:maxRows 未配置时用默认上限,避免 0 行+Truncated 的陷阱
	}
	coll := d.client.Database(d.dbName).Collection(q.Collection)
	var cur *mongo.Cursor
	if len(q.Pipeline) > 0 {
		// 使用 aggregate
		pipeline := make([]bson.M, len(q.Pipeline))
		copy(pipeline, q.Pipeline)
		cur, err = coll.Aggregate(ctx, pipeline)
	} else {
		// 使用 find
		cur, err = coll.Find(ctx, q.Find, options.Find().SetLimit(int64(rowCap)))
	}
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	res := &model.QueryResult{Columns: []string{"document"}, Rows: [][]any{}}
	for cur.Next(ctx) {
		if len(res.Rows) >= rowCap {
			res.Truncated = true
			break
		}
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		js, _ := json.Marshal(doc)
		res.Rows = append(res.Rows, []any{string(js)})
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	res.RowCount = len(res.Rows)
	return res, nil
}

// Exec 透传原始 Mongo 命令(JSON)给 RunCommand 执行,返回结果中的影响数(若有 n 字段)。
// 不封装 insertOne/updateMany 语法糖——由调用方写完整命令文档。
func (d *Driver) Exec(ctx context.Context, command string) (int64, error) {
	var cmd bson.D
	// canonical=false:接受 relaxed 扩展 JSON(如 {"$date":"..."}),尽量宽松地接纳手写命令。
	if err := bson.UnmarshalExtJSON([]byte(command), false, &cmd); err != nil {
		return 0, fmt.Errorf("mongo exec: command must be JSON command document: %v", err)
	}
	var result bson.M
	if err := d.client.Database(d.dbName).RunCommand(ctx, cmd).Decode(&result); err != nil {
		return 0, err
	}
	if n, ok := result["n"]; ok {
		switch v := n.(type) {
		case int32:
			return int64(v), nil
		case int64:
			return v, nil
		case float64:
			return int64(v), nil
		}
	}
	return 0, nil
}

// Close 释放底层 MongoDB 连接。
func (d *Driver) Close() error {
	return d.client.Disconnect(context.Background())
}

// formatFields 将采样字段格式化为可读的伪 DDL 字符串。
func formatFields(name string, fields map[string]string) string {
	out := "COLLECTION " + name + " (sampled) {\n"
	for k, typ := range fields {
		out += fmt.Sprintf("  %s: %s\n", k, typ)
	}
	return out + "}"
}

// databaseFromURI 从 MongoDB URI 中提取数据库名。
// 策略：跳过 scheme（mongodb:// 或 mongodb+srv://），找到 host 段之后的第一个 '/'，
// 取其后到 '?' 之前的部分作为数据库名。
//
// 示例：
//   - mongodb://h:27017/mydb          → mydb
//   - mongodb://u:p@h:27017/mydb?x=1  → mydb
//   - mongodb://h:27017               → ""
//   - mongodb+srv://h/mydb            → mydb
func databaseFromURI(uri string) string {
	// 去掉 scheme 前缀（mongodb:// 或 mongodb+srv://）
	rest := uri
	for _, scheme := range []string{"mongodb+srv://", "mongodb://"} {
		if strings.HasPrefix(uri, scheme) {
			rest = uri[len(scheme):]
			break
		}
	}
	// rest 现在是 [user:pass@]host[:port][/dbname][?options]
	// 找第一个 '/'（即 host 段之后的 '/'）
	idx := strings.Index(rest, "/")
	if idx < 0 {
		// 没有 '/'，没有数据库名
		return ""
	}
	dbPart := rest[idx+1:] // dbname[?options]
	// 去掉 query string
	if qi := strings.Index(dbPart, "?"); qi >= 0 {
		dbPart = dbPart[:qi]
	}
	return dbPart
}
