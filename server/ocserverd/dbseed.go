package main

// dbseed.go — the out-of-box roster seed (the dal/seed.py twin, reshaped by
// the single-owner decree): no owner row (the owner table is GONE from the Go
// ontology), just the two seed members —
//
//   * Mira, the assistant (offline out of the box; role_key == the admin role
//     the RBAC resolver keys on);
//   * the SERVER-SELF warden (id == the well-known machine id m-server-self):
//     a warden member whose own id IS the machine id — always present, listed
//     first in the machine panel, never deletable.
//
// Idempotent (skips existing rows), run at migrate AND at serve start so a
// fresh database always boots with the seed roster.

// seedMiraID is the seed assistant's member id (dal.seed.SEED_MIRA_ID).
const seedMiraID = "mira"

// seedServerSelfDisplay mirrors dal.seed.SEED_SERVER_SELF_DISPLAY.
const seedServerSelfDisplay = "伺服器這一台"

// seedOutOfBox inserts the out-of-box Mira + server-self warden rows if
// absent (idempotent — the dal.seed.seed_out_of_box twin, sans owner row).
func seedOutOfBox(d *DAL) error {
	mira, err := d.GetMember(seedMiraID)
	if err != nil {
		return err
	}
	if mira == nil {
		if err := d.PutMember(Member{
			ID:               seedMiraID,
			Name:             "Mira",
			Kind:             KindAssistant,
			RoleKey:          seedRoleAssistant,
			Effort:           "medium",
			DesiredState:     DesiredStateOffline,
			DesiredMachineID: ServerSelfHost,
			RosterStatus:     RosterStatusActive,
		}); err != nil {
			return err
		}
	}
	self, err := d.GetMember(ServerSelfHost)
	if err != nil {
		return err
	}
	if self == nil {
		// The warden carries NO self-binding (desired_machine_id "") — routing
		// resolves a warden by get_member of its own id == the machine id.
		if err := d.PutMember(Member{
			ID:               ServerSelfHost,
			Name:             seedServerSelfDisplay,
			Kind:             KindWarden,
			Effort:           "medium",
			DesiredState:     DesiredStateOffline,
			DesiredMachineID: "",
			RosterStatus:     RosterStatusActive,
		}); err != nil {
			return err
		}
	}
	return nil
}
