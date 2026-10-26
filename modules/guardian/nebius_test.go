package guardian

import (
    "context"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestNebiusPlannerSendsRealInferenceRequest(t *testing.T) {
    called:=0
    srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        called++
        if r.URL.Path!="/v1/chat/completions" || r.Header.Get("Authorization")!="Bearer test-key" {
            t.Errorf("incorrect request path=%q auth=%q",r.URL.Path,r.Header.Get("Authorization"))
        }
        w.Header().Set("Content-Type","application/json")
        fmt.Fprint(w,`{"choices":[{"message":{"content":"{\"action\":\"restore_replicas\",\"desired_replicas\":2,\"explanation\":\"service has zero replicas\"}"}}]}`)
    }))
    defer srv.Close()
    p:=NebiusPlanner{APIKey:"test-key",BaseURL:srv.URL+"/v1",Model:"nvidia/nemotron-3-super-120b-a12b",Client:srv.Client()}
    proposal,err:=p.Propose(context.Background(),Incident{Deployment:Deployment{Name:"api",Namespace:"demo",UID:"u"},Symptom:"HTTP 503"})
    if err!=nil || called!=1 || proposal.DesiredReplicas!=2 || proposal.Action!="restore_replicas" {
        t.Fatalf("proposal=%+v called=%d err=%v",proposal,called,err)
    }
}

func TestNebiusPlannerRejectsMalformedOrUnsafePlans(t *testing.T) {
    for _,content:=range []string{
        "not json",
        `{"action":"restore_replicas","desired_replicas":2,"explanation":"x","command":"rm -rf /"}`,
        `{"action":"restore_replicas","desired_replicas":2,"explanation":""}`,
    } {
        t.Run(content,func(t *testing.T){
            srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
                fmt.Fprintf(w,`{"choices":[{"message":{"content":%q}}]}`,content)
            }))
            defer srv.Close()
            p:=NebiusPlanner{APIKey:"test-key",BaseURL:srv.URL+"/v1",Client:srv.Client()}
            if _,err:=p.Propose(context.Background(),Incident{});err==nil {
                t.Fatal("accepted invalid model plan")
            }
        })
    }
}

func TestNebiusPlannerErrorsCannotAuthorizeWrites(t *testing.T) {
    p:=NebiusPlanner{APIKey:"",BaseURL:DefaultNebiusBaseURL}
    _,err:=p.Propose(context.Background(),Incident{})
    if err==nil || !strings.Contains(err.Error(),"NEBIUS_API_KEY") { t.Fatal("missing API key should fail") }
}
