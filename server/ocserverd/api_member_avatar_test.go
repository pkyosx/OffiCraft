package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var avatarTestPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
var avatarTestJPEG = []byte{0xff, 0xd8, 0xff, 0x00}
var avatarTestWebP = []byte{'R', 'I', 'F', 'F', 0x04, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}

func avatarRequest(method, target string, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	return req
}

func TestMemberAvatarReplaceRemoveAndPersistence(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-avatar")
	putTestMember(t, s, m)

	first := httptest.NewRecorder()
	s.HandlePutMemberAvatarApiMembersMemberIdAvatarPut(
		first,
		avatarRequest(http.MethodPut, "/api/members/m-avatar/avatar?mime=image/png", avatarTestPNG),
		m.ID,
		HandlePutMemberAvatarApiMembersMemberIdAvatarPutParams{Mime: stringPtr("image/png")},
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first upload: %d %s", first.Code, first.Body.String())
	}
	var firstDTO MemberAvatarDTO
	if err := json.Unmarshal(first.Body.Bytes(), &firstDTO); err != nil {
		t.Fatalf("decode first upload: %v", err)
	}
	if firstDTO.AvatarUrl == nil || !strings.HasPrefix(*firstDTO.AvatarUrl, "/api/chat/attachment/ava-") {
		t.Fatalf("personal URL must use a fresh dedicated blob: %+v", firstDTO)
	}
	firstID := strings.TrimPrefix(*firstDTO.AvatarUrl, "/api/chat/attachment/")
	if blob, err := s.dal.GetChatAttachment(firstID); err != nil || blob == nil {
		t.Fatalf("first blob must persist: %v %+v", err, blob)
	}

	second := httptest.NewRecorder()
	s.HandlePutMemberAvatarApiMembersMemberIdAvatarPut(
		second,
		avatarRequest(http.MethodPut, "/api/members/m-avatar/avatar?mime=image/jpeg", avatarTestJPEG),
		m.ID,
		HandlePutMemberAvatarApiMembersMemberIdAvatarPutParams{Mime: stringPtr("image/jpeg")},
	)
	if second.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", second.Code, second.Body.String())
	}
	var secondDTO MemberAvatarDTO
	if err := json.Unmarshal(second.Body.Bytes(), &secondDTO); err != nil {
		t.Fatalf("decode replace: %v", err)
	}
	secondID := strings.TrimPrefix(*secondDTO.AvatarUrl, "/api/chat/attachment/")
	if secondID == firstID {
		t.Fatal("replacement must mint a cache-busting blob id")
	}
	if old, err := s.dal.GetChatAttachment(firstID); err != nil || old != nil {
		t.Fatalf("replacement must delete old blob: %v %+v", err, old)
	}
	stored, err := s.dal.GetMember(m.ID)
	if err != nil || stored == nil || stored.AvatarAttachmentID != secondID {
		t.Fatalf("stable member pointer must persist: %v %+v", err, stored)
	}
	if got := s.newMemberDTO(*stored, "", "", 0).AvatarURL; got != *secondDTO.AvatarUrl {
		t.Fatalf("member DTO URL = %q, want %q", got, *secondDTO.AvatarUrl)
	}

	remove := httptest.NewRecorder()
	s.HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete(
		remove, avatarRequest(http.MethodDelete, "/api/members/m-avatar/avatar", nil), m.ID,
	)
	if remove.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", remove.Code, remove.Body.String())
	}
	stored, err = s.dal.GetMember(m.ID)
	if err != nil || stored == nil || stored.AvatarAttachmentID != "" {
		t.Fatalf("remove must clear durable pointer: %v %+v", err, stored)
	}
	if old, err := s.dal.GetChatAttachment(secondID); err != nil || old != nil {
		t.Fatalf("remove must delete blob: %v %+v", err, old)
	}

	again := httptest.NewRecorder()
	s.HandleDeleteMemberAvatarApiMembersMemberIdAvatarDelete(
		again, avatarRequest(http.MethodDelete, "/api/members/m-avatar/avatar", nil), m.ID,
	)
	if again.Code != http.StatusOK {
		t.Fatalf("idempotent remove: %d %s", again.Code, again.Body.String())
	}
}

func TestMemberAvatarValidationAndTargetGuards(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-avatar")
	putTestMember(t, s, m)
	warden := testAgent("m-machine")
	warden.Kind = KindWarden
	putTestMember(t, s, warden)

	cases := []struct {
		name   string
		id     string
		body   []byte
		mime   *string
		status int
	}{
		{"empty", m.ID, nil, stringPtr("image/png"), http.StatusUnprocessableEntity},
		{"magic bytes without declared mime", m.ID, []byte("<svg onload=alert(1)>"), nil, http.StatusUnprocessableEntity},
		{"svg or arbitrary bytes", m.ID, []byte("<svg onload=alert(1)>"), stringPtr("image/svg+xml"), http.StatusUnprocessableEntity},
		{"mime mismatch", m.ID, avatarTestJPEG, stringPtr("image/png"), http.StatusUnprocessableEntity},
		{"oversize", m.ID, append(append([]byte{}, avatarTestPNG...), make([]byte, maxAvatarBytes)...), stringPtr("image/png"), http.StatusRequestEntityTooLarge},
		{"machine target", warden.ID, avatarTestPNG, stringPtr("image/png"), http.StatusUnprocessableEntity},
		{"unknown target", "m-missing", avatarTestPNG, stringPtr("image/png"), http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.HandlePutMemberAvatarApiMembersMemberIdAvatarPut(
				rec,
				avatarRequest(http.MethodPut, "/api/members/"+tc.id+"/avatar", tc.body),
				tc.id,
				HandlePutMemberAvatarApiMembersMemberIdAvatarPutParams{Mime: tc.mime},
			)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestMemberAvatarAcceptedFormatsAndSizeBoundary(t *testing.T) {
	exactLimitPNG := append([]byte{}, avatarTestPNG...)
	exactLimitPNG = append(exactLimitPNG, make([]byte, maxAvatarBytes-len(exactLimitPNG))...)
	overLimitPNG := append(append([]byte{}, exactLimitPNG...), 0x00)

	cases := []struct {
		name     string
		body     []byte
		mime     string
		status   int
		wantMime string
	}{
		{"webp happy path", avatarTestWebP, "image/webp", http.StatusOK, "image/webp"},
		{"exactly 64 KiB", exactLimitPNG, "image/png", http.StatusOK, "image/png"},
		{"64 KiB plus one", overLimitPNG, "image/png", http.StatusRequestEntityTooLarge, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			m := testAgent("m-avatar-" + strings.ReplaceAll(tc.name, " ", "-"))
			putTestMember(t, s, m)
			rec := httptest.NewRecorder()
			s.HandlePutMemberAvatarApiMembersMemberIdAvatarPut(
				rec,
				avatarRequest(http.MethodPut, "/api/members/"+m.ID+"/avatar", tc.body),
				m.ID,
				HandlePutMemberAvatarApiMembersMemberIdAvatarPutParams{Mime: &tc.mime},
			)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status != http.StatusOK {
				return
			}
			var got MemberAvatarDTO
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.Mime == nil || *got.Mime != tc.wantMime || got.AvatarUrl == nil {
				t.Fatalf("accepted upload response = %+v, want mime %q and URL", got, tc.wantMime)
			}
			id := strings.TrimPrefix(*got.AvatarUrl, "/api/chat/attachment/")
			blob, err := s.dal.GetChatAttachment(id)
			if err != nil || blob == nil || len(blob.Data) != len(tc.body) {
				t.Fatalf("stored blob = %+v err=%v, want %d bytes", blob, err, len(tc.body))
			}
		})
	}
}

func TestMemberAvatarRoutesAreOwnerOnlyAndOffMCP(t *testing.T) {
	wrapper := &ServerInterfaceWrapper{}
	var found int
	for _, route := range routeSpecs(wrapper) {
		if route.Path != "/api/members/{member_id}/avatar" {
			continue
		}
		found++
		if route.Requires != principalOwner {
			t.Errorf("%s requires %q, want owner", route.Method, route.Requires)
		}
		if !route.MCPExclude {
			t.Errorf("%s avatar binary seam must stay off MCP", route.Method)
		}
	}
	if found != 2 {
		t.Fatalf("avatar route count = %d, want PUT + DELETE", found)
	}
}

func TestHardDeleteMemberRemovesOwnedAvatarBlob(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-avatar-delete")
	putTestMember(t, s, m)
	avatar := ChatAttachment{
		ID: "ava-hard-delete", Mime: "image/png", Data: avatarTestPNG,
	}
	if err := s.dal.ReplaceMemberAvatar(m.ID, avatar); err != nil {
		t.Fatalf("seed avatar: %v", err)
	}

	deleted, err := s.dal.HardDeleteMember(m.ID)
	if err != nil || !deleted {
		t.Fatalf("hard delete: deleted=%v err=%v", deleted, err)
	}
	if blob, err := s.dal.GetChatAttachment(avatar.ID); err != nil || blob != nil {
		t.Fatalf("owned avatar blob must be deleted with member: %v %+v", err, blob)
	}
}

func TestReplaceMemberAvatarRollsBackPointerAndBlobsOnMemberWriteFailure(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-avatar-rollback")
	putTestMember(t, s, m)
	previous := ChatAttachment{
		ID: "ava-rollback-previous", Mime: "image/png", Data: avatarTestPNG,
	}
	if err := s.dal.ReplaceMemberAvatar(m.ID, previous); err != nil {
		t.Fatalf("seed previous avatar: %v", err)
	}

	disarm := breakWrites(t, s.dal.wdb, "member")
	next := ChatAttachment{
		ID: "ava-rollback-next", Mime: "image/webp", Data: avatarTestWebP,
	}
	if err := s.dal.ReplaceMemberAvatar(m.ID, next); err == nil {
		disarm()
		t.Fatal("replace must fail when the member pointer update is injected to fail")
	}
	disarm()

	fresh, err := s.dal.GetMember(m.ID)
	if err != nil || fresh == nil || fresh.AvatarAttachmentID != previous.ID {
		t.Fatalf("failed replace changed member pointer: %v %+v", err, fresh)
	}
	if blob, err := s.dal.GetChatAttachment(previous.ID); err != nil || blob == nil {
		t.Fatalf("failed replace deleted previous blob: %v %+v", err, blob)
	}
	if blob, err := s.dal.GetChatAttachment(next.ID); err != nil || blob != nil {
		t.Fatalf("failed replace leaked the new blob: %v %+v", err, blob)
	}
}

func TestMemberAvatarRejectsReuseByGeneralAttachmentGraphs(t *testing.T) {
	s := newReconcileTestServer(t)
	avatarID := "ava-single-owner"
	if err := s.dal.PutChatAttachment(ChatAttachment{
		ID: avatarID, Mime: "image/png", Data: avatarTestPNG,
	}); err != nil {
		t.Fatalf("seed avatar blob: %v", err)
	}

	if _, status, problem := s.resolveChatAttachmentInputs([]ChatAttachmentInputDTO{
		{Id: &avatarID},
	}); status != http.StatusBadRequest || !strings.Contains(problem, "reserved") {
		t.Fatalf("chat/reply/task-message ref must reject avatar id: status=%d problem=%q", status, problem)
	}

	taskAPI := newTasksTestServer(t)
	task := createAdHocTask(t, taskAPI, "m-exec")
	if err := taskAPI.dal.PutChatAttachment(ChatAttachment{
		ID: avatarID, Mime: "image/png", Data: avatarTestPNG,
	}); err != nil {
		t.Fatalf("seed task-side avatar blob: %v", err)
	}
	rec := addArtifact(t, taskAPI, task.ID,
		map[string]any{"kind": "image", "attachment_id": avatarID},
		"m-exec", "agent")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reserved") {
		t.Fatalf("task artifact must reject avatar id: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPutMemberStaleSnapshotCannotEraseNewerAvatar(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-avatar-stale")
	putTestMember(t, s, m)
	stale, err := s.dal.GetMember(m.ID)
	if err != nil || stale == nil {
		t.Fatalf("read stale member snapshot: %v %+v", err, stale)
	}
	avatar := ChatAttachment{
		ID: "ava-newer-than-snapshot", Mime: "image/png", Data: avatarTestPNG,
	}
	if err := s.dal.ReplaceMemberAvatar(m.ID, avatar); err != nil {
		t.Fatalf("replace avatar: %v", err)
	}

	stale.Name = "unrelated rename from stale snapshot"
	if err := s.dal.PutMember(*stale); err != nil {
		t.Fatalf("write stale member snapshot: %v", err)
	}
	fresh, err := s.dal.GetMember(m.ID)
	if err != nil || fresh == nil || fresh.AvatarAttachmentID != avatar.ID {
		t.Fatalf("general upsert erased avatar pointer: %v %+v", err, fresh)
	}
	if blob, err := s.dal.GetChatAttachment(avatar.ID); err != nil || blob == nil {
		t.Fatalf("general upsert orphaned avatar blob: %v %+v", err, blob)
	}
}

func TestMemberAvatarPublishesKindSpecificSSETopic(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  string
		topic string
	}{
		{name: "staff", kind: KindStaff, topic: "member"},
		{name: "outsource", kind: KindOutsource, topic: "outsource_worker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			listener, err := s.hub.Connect("", "")
			if err != nil {
				t.Fatalf("connect owner listener: %v", err)
			}
			m := testAgent("m-avatar-" + tc.name)
			m.Kind = tc.kind
			putTestMember(t, s, m)
			rec := httptest.NewRecorder()
			s.HandlePutMemberAvatarApiMembersMemberIdAvatarPut(
				rec,
				avatarRequest(http.MethodPut, "/api/members/"+m.ID+"/avatar", avatarTestPNG),
				m.ID,
				HandlePutMemberAvatarApiMembersMemberIdAvatarPutParams{},
			)
			if rec.Code != http.StatusOK {
				t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
			}
			frame := string(listener.pop())
			if !strings.Contains(frame, `"topic":"`+tc.topic+`"`) {
				t.Fatalf("SSE frame topic = %q, want %q", frame, tc.topic)
			}
		})
	}
}

func stringPtr(value string) *string { return &value }
