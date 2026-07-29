package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMemberAvatarIndexUpdatePersistsAndRejectsInvalidTargets(t *testing.T) {
	s := newTasksTestServer(t)
	staff := Member{
		ID: "m-avatar-index", Name: "Index", Kind: KindAssistant,
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}
	if err := s.dal.PutMember(staff); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/members/m-avatar-index/avatar-index",
		strings.NewReader(`{"avatar_index":7}`))
	rec := httptest.NewRecorder()
	s.HandleUpdateMemberAvatarIndexApiMembersMemberIdAvatarIndexPatch(rec, req, staff.ID)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"avatar_index":7,"member_id":"m-avatar-index"}` {
		t.Fatalf("update: status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := s.dal.GetMember(staff.ID)
	if err != nil || stored == nil || stored.AvatarIndex != 7 {
		t.Fatalf("stored index: member=%+v err=%v", stored, err)
	}

	for _, tc := range []struct {
		name, id, body string
		want           int
	}{
		{"negative", staff.ID, `{"avatar_index":-1}`, http.StatusUnprocessableEntity},
		{"missing", staff.ID, `{}`, http.StatusUnprocessableEntity},
		{"unknown", "m-missing", `{"avatar_index":1}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/avatar-index", strings.NewReader(tc.body))
			s.HandleUpdateMemberAvatarIndexApiMembersMemberIdAvatarIndexPatch(rec, req, tc.id)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestMemberAvatarIndexDoesNotRaceFullMemberSnapshots(t *testing.T) {
	s := newTasksTestServer(t)
	staff := Member{
		ID: "m-avatar-race", Name: "Before", Kind: KindAssistant,
		Runtime: RuntimeClaude, DesiredState: DesiredStateOnline,
		RosterStatus: RosterStatusActive,
	}
	if err := s.dal.PutMember(staff); err != nil {
		t.Fatal(err)
	}
	stale, err := s.dal.GetMember(staff.ID)
	if err != nil || stale == nil {
		t.Fatalf("capture stale snapshot: member=%+v err=%v", stale, err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/avatar-index",
		strings.NewReader(`{"avatar_index":7}`))
	s.HandleUpdateMemberAvatarIndexApiMembersMemberIdAvatarIndexPatch(rec, req, staff.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("avatar update: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// A lifecycle writer holding the pre-edit snapshot may update its own
	// fields, but its stale AvatarIndex=0 must not undo the owner edit.
	stale.Name = "After lifecycle write"
	stale.DesiredState = DesiredStateOffline
	if err := s.dal.PutMember(*stale); err != nil {
		t.Fatal(err)
	}
	stored, err := s.dal.GetMember(staff.ID)
	if err != nil || stored == nil {
		t.Fatalf("read after lifecycle write: member=%+v err=%v", stored, err)
	}
	if stored.AvatarIndex != 7 {
		t.Fatalf("stale full snapshot clobbered avatar index: %+v", stored)
	}
	if stored.Name != "After lifecycle write" || stored.DesiredState != DesiredStateOffline {
		t.Fatalf("lifecycle write did not preserve its own fields: %+v", stored)
	}

	// The inverse is narrow too: changing presentation state leaves lifecycle
	// fields untouched.
	if ok, err := s.dal.UpdateMemberAvatarIndex(staff.ID, 9); err != nil || !ok {
		t.Fatalf("narrow avatar update: ok=%v err=%v", ok, err)
	}
	stored, err = s.dal.GetMember(staff.ID)
	if err != nil || stored == nil || stored.AvatarIndex != 9 ||
		stored.Name != "After lifecycle write" || stored.DesiredState != DesiredStateOffline {
		t.Fatalf("avatar update clobbered lifecycle state: member=%+v err=%v", stored, err)
	}
}

func TestMemberAvatarIndexAcceptsOutsourceAndRejectsWarden(t *testing.T) {
	s := newTasksTestServer(t)
	worker := OutsourceWorker{
		ID: "ow-avatar-index", Codename: "O-1", Runtime: RuntimeClaude,
		Status: WorkerStatusAssigned, DesiredState: DesiredStateOnline,
	}
	if err := s.dal.PutOutsourceWorker(worker); err != nil {
		t.Fatal(err)
	}
	warden := Member{
		ID: "mac-avatar-index", Name: "Mac", Kind: KindWarden,
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}
	if err := s.dal.PutMember(warden); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		want int
	}{
		{worker.ID, http.StatusOK},
		{warden.ID, http.StatusUnprocessableEntity},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/avatar-index",
			strings.NewReader(`{"avatar_index":3}`))
		s.HandleUpdateMemberAvatarIndexApiMembersMemberIdAvatarIndexPatch(rec, req, tc.id)
		if rec.Code != tc.want {
			t.Fatalf("%s: status=%d body=%s", tc.id, rec.Code, rec.Body.String())
		}
	}
}

func TestMemberAvatarIndexRouteIsOwnerOnlyAndOffMCP(t *testing.T) {
	s := newTasksTestServer(t)
	for _, route := range specsFor(s) {
		if route.Path != "/api/members/{member_id}/avatar-index" {
			continue
		}
		if route.Method != http.MethodPatch || route.Requires != principalOwner || !route.MCPExclude {
			t.Fatalf("unexpected route: %+v", route)
		}
		return
	}
	t.Fatal("avatar-index route missing")
}

func TestOutsourceWorkerDeltaCarriesAvatarIndex(t *testing.T) {
	s := newTasksTestServer(t)
	cockpit, err := s.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.hub.Disconnect(cockpit)

	s.publishOutsourceWorker(OutsourceWorker{
		ID: "ow-avatar-event", Codename: "O-7",
		Status: WorkerStatusAssigned, AvatarIndex: 6,
	}, triggerServer)
	frame := cockpit.pop()
	if frame == nil {
		t.Fatal("owner cockpit received no outsource_worker delta")
	}
	text := string(frame)
	if !strings.Contains(text, `"topic":"outsource_worker"`) ||
		!strings.Contains(text, `"avatar_index":6`) {
		t.Fatalf("outsource_worker delta does not carry avatar_index: %s", text)
	}
}

func TestActiveAvatarPoolSizeAndRandomIndexStayInRange(t *testing.T) {
	s := newTasksTestServer(t)
	memberPool := []string{"one", "two", "three"}
	outsourcePool := []string{"one", "two"}
	pools := map[string][]string{
		"member": memberPool, "outsource": outsourcePool,
	}
	s.displayTheme = "portraits"
	s.displayCustomThemes = []ThemeBundleDTO{{
		Id: "portraits", Name: "Portraits",
		Colors:      map[string]string{"--color-bg": "#000000"},
		AvatarPools: &pools,
	}}
	for _, tc := range []struct {
		kind string
		want int
	}{
		{KindAssistant, 3},
		{KindOutsource, 2},
		{KindWarden, 0},
	} {
		if got := s.activeAvatarPoolSize(tc.kind); got != tc.want {
			t.Fatalf("%s pool size=%d, want %d", tc.kind, got, tc.want)
		}
		for range 100 {
			index := RandomAvatarIndex(tc.want)
			if index < 0 || (tc.want == 0 && index != 0) ||
				(tc.want > 0 && index >= tc.want) {
				t.Fatalf("%s random index=%d outside pool len %d", tc.kind, index, tc.want)
			}
		}
	}
}
