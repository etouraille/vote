package main

import (
	"reflect"
	"testing"

	"github.com/etouraille/queel/rbac"
)

// TestPermissionsRequestCoversEveryPermission pins the one thing that can
// go wrong silently here: permissionsRequest restates rbac.Permissions
// field by field, and assignPermissionsHandler copies them across by hand.
// A permission added to rbac but not to both is not a compile error — the
// backoffice simply shows a checkbox that never saves, which is how
// canSubscribe first shipped.
//
// Compared by JSON tag rather than by Go field name: the tag is what the
// front end actually sends, so a mismatched tag is the same bug wearing a
// different hat.
func TestPermissionsRequestCoversEveryPermission(t *testing.T) {
	want := jsonTags(reflect.TypeOf(rbac.Permissions{}))
	got := jsonTags(reflect.TypeOf(permissionsRequest{}))

	// Root rides along on the request but isn't a Permissions field — it's
	// a separate flag on rbac.User.
	delete(got, "root")

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("permissionsRequest does not mirror rbac.Permissions:\n  rbac.Permissions:    %v\n  permissionsRequest: %v", keys(want), keys(got))
	}
}

func jsonTags(t reflect.Type) map[string]bool {
	tags := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" {
			tag = t.Field(i).Name
		}
		tags[tag] = true
	}
	return tags
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
