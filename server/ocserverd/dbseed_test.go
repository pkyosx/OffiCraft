// Skeleton generated from server/ocserverd/dbseed.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"reflect"
	"testing"
)

func TestSeedOutOfBox(t *testing.T) {
	d := newAPITestDAL(t)
	if err := seedOutOfBox(d); err != nil {
		t.Fatalf("seedOutOfBox: %v", err)
	}

	mira, err := d.GetMember(seedMiraID)
	if err != nil {
		t.Fatalf("GetMember(mira): %v", err)
	}
	if mira == nil {
		t.Fatal("GetMember(mira): no row")
	}
	if want := (Member{
		ID:               "mira",
		Name:             "Mira",
		Kind:             KindStaff,
		RoleKey:          seedRoleAssistant,
		Effort:           "medium",
		DesiredState:     DesiredStateOffline,
		DesiredMachineID: ServerSelfHost,
		RosterStatus:     RosterStatusActive,
	}); !reflect.DeepEqual(*mira, want) {
		t.Fatalf("Mira row = %+v, want %+v", *mira, want)
	}

	self, err := d.GetMember(ServerSelfHost)
	if err != nil {
		t.Fatalf("GetMember(server-self): %v", err)
	}
	if self == nil {
		t.Fatal("GetMember(server-self): no row")
	}
	if want := (Member{
		ID:           ServerSelfHost,
		Name:         seedServerSelfDisplay,
		Kind:         KindWarden,
		Effort:       "medium",
		DesiredState: DesiredStateOffline,
		RosterStatus: RosterStatusActive,
	}); !reflect.DeepEqual(*self, want) {
		t.Fatalf("server-self row = %+v, want %+v", *self, want)
	}

	mira.Name = "Owner's Mira"
	self.Name = "Renamed machine"
	if err := d.PutMember(*mira); err != nil {
		t.Fatalf("change Mira: %v", err)
	}
	if err := d.PutMember(*self); err != nil {
		t.Fatalf("change server-self: %v", err)
	}
	if err := seedOutOfBox(d); err != nil {
		t.Fatalf("second seedOutOfBox: %v", err)
	}

	mira, err = d.GetMember(seedMiraID)
	if err != nil || mira == nil || mira.Name != "Owner's Mira" {
		t.Fatalf("second seed changed Mira: row=%+v err=%v", mira, err)
	}
	self, err = d.GetMember(ServerSelfHost)
	if err != nil || self == nil || self.Name != "Renamed machine" {
		t.Fatalf("second seed changed server-self: row=%+v err=%v", self, err)
	}
}
