package main

import (
	"reflect"
	"testing"
)

// «Работали» of a design are people by name — its owner, then the person of each of its chats, a delegated
// chat's being its root's — each once, never a chat id or an address.
func TestDesignPeopleNamesPeopleOnly(t *testing.T) {
	personOf := map[string]string{
		"tab-a1": "timur@team.test",
		"tab-b2": "",
		"tab-c3": "timur@team.test",
		"tab-d4": "vera@team.test",
	}
	nameOf := func(address string) string {
		return map[string]string{"timur@team.test": "Тимур", "": "Глеб", "vera@team.test": "Вера"}[address]
	}
	got := designPeople("Тимур", []string{"tab-a1", "tab-b2", "tab-c3", "tab-gone", "tab-d4"}, personOf, nameOf)
	if want := []string{"Тимур", "Глеб", "Вера"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("people %v, want %v", got, want)
	}
	leak := designPeople("", []string{"tab-x"}, map[string]string{"tab-x": "x"}, func(string) string { return "tab-k1yksasiw" })
	if leak != nil {
		t.Fatalf("an id passed as a name: %v", leak)
	}
}
