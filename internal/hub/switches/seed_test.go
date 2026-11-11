package switches

import "testing"

func TestParseSeeds(t *testing.T) {
	rows, err := ParseSeeds(`[{"feature":"fleet-stale","person":"a@example.com","enabled":true,"note":"first"},{"feature":"x","enabled":false}]`)
	if err != nil || len(rows) != 2 || rows[0].Person != "a@example.com" || !rows[0].Enabled || rows[1].Machine != "" {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	if _, err := ParseSeeds(`[{"person":"a@example.com","enabled":true}]`); err == nil {
		t.Fatal("a row without a feature is accepted")
	}
	if _, err := ParseSeeds(`{"feature":"x"}`); err == nil {
		t.Fatal("an object instead of an array is accepted")
	}
}
