package store

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestEveryTableHasAnOwnerAndATerm(t *testing.T) {
	kindOf := map[string]string{}
	for _, k := range DataKinds {
		if k.Name == "" || k.Keep == "" || k.OwnerDeletes == "" || len(k.Tables) == 0 {
			t.Errorf("kind %q: name, tables, term and what a delete does are all required", k.Name)
		}
		switch k.Owner {
		case OwnerMachine, OwnerHub, OwnerOutside:
		default:
			t.Errorf("kind %q: unknown owner %q", k.Name, k.Owner)
		}
		for _, tbl := range k.Tables {
			if was, ok := kindOf[tbl]; ok {
				t.Errorf("table %s is in two kinds: %q and %q", tbl, was, k.Name)
			}
			kindOf[tbl] = k.Name
		}
	}
	inSchema := map[string]bool{}
	for _, driver := range []Driver{SQLite, Postgres} {
		s, err := ExpectedSchema(driver)
		if err != nil {
			t.Fatal(err)
		}
		var missing []string
		for name := range s.Tables {
			inSchema[name] = true
			if _, ok := kindOf[name]; !ok {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: tables without an owner: %v. Add each to a kind of DataKinds (owners.go) with who owns the data, how long it is kept and what a delete at the owner does", driver, missing)
		}
	}
	var stale []string
	for tbl := range kindOf {
		if !inSchema[tbl] {
			stale = append(stale, tbl)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("tables in DataKinds that the schema does not have: %v", stale)
	}
}

func TestTakeLease(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	now := time.UnixMilli(1_800_000_000_000)
	take := func(holder string, at time.Time) bool {
		t.Helper()
		ok, err := db.S().TakeLease(ctx, "job", holder, at, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !take("a", now) {
		t.Fatal("a free job was not taken")
	}
	if take("b", now.Add(30*time.Second)) {
		t.Fatal("a held job was taken by another holder")
	}
	if !take("a", now.Add(30*time.Second)) {
		t.Fatal("the holder did not extend its own lease")
	}
	if take("b", now.Add(80*time.Second)) {
		t.Fatal("the extended lease did not hold")
	}
	if !take("b", now.Add(91*time.Second)) {
		t.Fatal("a lease that ran out was not taken")
	}
	if take("a", now.Add(92*time.Second)) {
		t.Fatal("the old holder took the job back while it is held")
	}
	if ok, err := db.S().TakeLease(ctx, "other", "a", now, time.Minute); err != nil || !ok {
		t.Fatalf("another job: %v %v", ok, err)
	}
}
