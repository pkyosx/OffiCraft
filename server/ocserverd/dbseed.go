package main

const seedMiraID = "mira"

const seedServerSelfDisplay = "伺服器這一台"

func seedOutOfBox(d *DAL) error {
	mira, err := d.GetMember(seedMiraID)
	if err != nil {
		return err
	}
	if mira == nil {
		if err := d.PutMember(Member{
			ID:               seedMiraID,
			Name:             "Mira",
			Kind:             KindStaff,
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
