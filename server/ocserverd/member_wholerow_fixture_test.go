package main

// Fixtures only. Production has no whole-row write onto an existing member row
// (CreateMember creates; edits are patches), but many tests set up state by
// landing a hand-built row over one that already exists. These keep that
// fixture shape: create if absent, else every column not flagged insertOnly.

func (d *DAL) putMemberWholeRowForTest(m Member) error {
	return d.inTx(func(tx *writeTx) error {
		fields := memberWholeRow(m)
		existing, err := getMemberOn(tx, m.ID)
		if err != nil {
			return err
		}
		if existing == nil {
			return insertMemberRow(tx, fields)
		}
		return patchMemberOn(tx, m.ID, updatableMemberFields(fields)...)
	})
}

func (d *DAL) PutOutsourceWorker(w OutsourceWorker) error {
	return d.putMemberWholeRowForTest(memberFromWorker(w))
}

func (s *apiServer) putMember(m Member, trigger string) error {
	if err := ValidateMember(m); err != nil {
		return err
	}
	if err := s.dal.putMemberWholeRowForTest(m); err != nil {
		return err
	}
	s.publishMemberPatch(m, trigger)
	return nil
}

func updatableMemberFields(fields []memberField) []memberField {
	out := make([]memberField, 0, len(fields))
	for _, f := range fields {
		if !f.insertOnly {
			out = append(out, f)
		}
	}
	return out
}
