package dbfactory

import "testing"

func TestOpenDriver_UnknownDriver(t *testing.T) {
	_, err := OpenDriver("prod", "weird", "dsn", "d")
	if err == nil {
		t.Fatal("unknown driver should error")
	}
}
