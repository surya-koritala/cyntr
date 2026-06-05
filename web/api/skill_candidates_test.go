package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/skill"
)

// fakeSkillRuntime answers the candidate IPC topics and records what it got.
func fakeSkillRuntime(t *testing.T) (*ipc.Bus, *struct {
	approvedID int64
	rejected   skill.RejectRequest
	listStatus string
}) {
	t.Helper()
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	rec := &struct {
		approvedID int64
		rejected   skill.RejectRequest
		listStatus string
	}{}
	bus.Handle("skill_runtime", skill.TopicCandidates, func(msg ipc.Message) (ipc.Message, error) {
		rec.listStatus, _ = msg.Payload.(string)
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: []skill.Candidate{
			{ID: 7, Name: "proposed-skill", Status: skill.CandidatePending},
		}}, nil
	})
	bus.Handle("skill_runtime", skill.TopicCandidateApprove, func(msg ipc.Message) (ipc.Message, error) {
		rec.approvedID, _ = msg.Payload.(int64)
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
	})
	bus.Handle("skill_runtime", skill.TopicCandidateReject, func(msg ipc.Message) (ipc.Message, error) {
		rec.rejected, _ = msg.Payload.(skill.RejectRequest)
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
	})
	return bus, rec
}

func TestSkillCandidatesList(t *testing.T) {
	bus, rec := fakeSkillRuntime(t)
	srv := NewServer(bus, nil)
	req := httptest.NewRequest("GET", "/api/v1/skills/candidates", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "proposed-skill") {
		t.Fatalf("candidate not listed: %s", w.Body.String())
	}
	_ = rec
}

func TestSkillCandidateApprove(t *testing.T) {
	bus, rec := fakeSkillRuntime(t)
	srv := NewServer(bus, nil)
	req := httptest.NewRequest("POST", "/api/v1/skills/candidates/7/approve", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if rec.approvedID != 7 {
		t.Fatalf("approve forwarded id %d, want 7", rec.approvedID)
	}
}

func TestSkillCandidateReject(t *testing.T) {
	bus, rec := fakeSkillRuntime(t)
	srv := NewServer(bus, nil)
	req := httptest.NewRequest("POST", "/api/v1/skills/candidates/7/reject", strings.NewReader(`{"reason":"low quality"}`))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if rec.rejected.ID != 7 || rec.rejected.Reason != "low quality" {
		t.Fatalf("reject forwarded %+v, want {7, low quality}", rec.rejected)
	}
}

func TestSkillCandidateBadID(t *testing.T) {
	bus, _ := fakeSkillRuntime(t)
	srv := NewServer(bus, nil)
	req := httptest.NewRequest("POST", "/api/v1/skills/candidates/notanumber/approve", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("non-numeric id should be 400, got %d", w.Code)
	}
}

func TestSkillCandidatesEnvelope(t *testing.T) {
	bus, _ := fakeSkillRuntime(t)
	srv := NewServer(bus, nil)
	req := httptest.NewRequest("GET", "/api/v1/skills/candidates?status=pending", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var env Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}
}
