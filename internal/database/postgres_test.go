package database

import "testing"

func TestOpenPostgresRejectsEmptyURL(t *testing.T) {
	db, err := OpenPostgres("")
	if err == nil {
		t.Fatal("open Postgres with empty URL: got nil error")
	}
	if db != nil {
		t.Fatal("open Postgres with empty URL: got non-nil database")
	}
}
