package guardian

import (
    "context"
    "errors"
    "fmt"
    "testing"
    "time"
)

type fakePlanner struct { proposal Proposal; err error; calls int }
func (p *fakePlanner) Propose(ctx context.Context, in Incident) (Proposal,error) {
    p.calls++
    return p.proposal,p.err
}

type fakeCluster struct {
    d Deployment
    writes int
    readyImmediately bool
    drift bool
}
func (c *fakeCluster) Read(ctx context.Context, ns,name string) (Deployment,error) {
    if ns!=c.d.Namespace || name!=c.d.Name { return Deployment{},errors.New("wrong workload") }
    d:=c.d
    if c.drift && c.writes>0 { d.UID="replacement-workload" }
    return d,nil
}
func (c *fakeCluster) Scale(ctx context.Context, ns,name,uid string,from,to int32) error {
    if ns!=c.d.Namespace || name!=c.d.Name || uid!=c.d.UID || c.d.Replicas!=from { return errors.New("CAS failed") }
    c.writes++
    c.d.Replicas=to
    if c.readyImmediately { c.d.Ready=to } else { c.d.Ready=0 }
    return nil
}
func fixture() (*Runner,*fakeCluster,*fakePlanner) {
    cluster:=&fakeCluster{d:Deployment{Name:"api",Namespace:"demo",UID:"uid-1234",Replicas:0,Ready:0},readyImmediately:true}
    planner:=&fakePlanner{proposal:Proposal{Action:"restore_replicas",DesiredReplicas:2,Explanation:"zero replicas and service unavailable"}}
    r:=&Runner{Cluster:cluster,Planner:planner,Namespace:"demo",Deployment:"api",
        Symptom:"HTTP 503 and zero endpoints",MaxReplicas:3,PollInterval:time.Millisecond,VerifyTimeout:8*time.Millisecond}
    return r,cluster,planner
}

func TestGuardianDryRunNeverWrites(t *testing.T) {
    r,c,p:=fixture()
    res,err:=r.Run(context.Background())
    if err!=nil || res.Status!="planned" || c.writes!=0 || p.calls!=1 {
        t.Fatalf("unexpected dry-run: res=%+v err=%v writes=%d calls=%d",res,err,c.writes,p.calls)
    }
}

func TestGuardianApprovedRecovery(t *testing.T) {
    r,c,_:=fixture()
    r.Execute=true
    r.Approval="scale:demo/api:2"
    res,err:=r.Run(context.Background())
    if err!=nil || res.Status!="recovered" || c.writes!=1 || c.d.Ready!=2 {
        t.Fatalf("recovery failed: res=%+v err=%v writes=%d",res,err,c.writes)
    }
    if len(res.Events)<4 { t.Fatalf("missing event evidence: %+v",res.Events) }
}

func TestGuardianRequiresActionBoundApproval(t *testing.T) {
    for _,approval:=range []string{"","scale:demo/api:1","scale:prod/api:2"} {
        t.Run(fmt.Sprintf("%q",approval),func(t *testing.T) {
            r,c,_:=fixture();r.Execute=true;r.Approval=approval
            res,err:=r.Run(context.Background())
            if err==nil || res.Status!="denied" || c.writes!=0 {
                t.Fatalf("approval bypass: res=%+v err=%v writes=%d",res,err,c.writes)
            }
        })
    }
}

func TestGuardianRefusesInvalidProposals(t *testing.T) {
    cases:=[]Proposal{
        {Action:"shell_exec",DesiredReplicas:2,Explanation:"unsafe"},
        {Action:"restore_replicas",DesiredReplicas:4,Explanation:"above cap"},
        {Action:"restore_replicas",DesiredReplicas:0,Explanation:"bad target"},
    }
    for _,p:=range cases {
        r,c,planner:=fixture();r.Execute=true;r.Approval="scale:demo/api:2";planner.proposal=p
        res,err:=r.Run(context.Background())
        if err==nil || res.Status!="rejected" || c.writes!=0 { t.Fatalf("proposal bypass: %+v err=%v",res,err) }
    }
}

func TestGuardianRollbackAfterVerificationFailure(t *testing.T) {
    r,c,_:=fixture()
    c.readyImmediately=false
    r.Execute=true;r.Approval="scale:demo/api:2"
    res,err:=r.Run(context.Background())
    if err==nil || res.Status!="rolled_back" || c.writes!=2 || c.d.Replicas!=0 {
        t.Fatalf("rollback mismatch: res=%+v err=%v writes=%d",res,err,c.writes)
    }
}

func TestGuardianRefusesRollbackOnConcurrentIdentityDrift(t *testing.T) {
    r,c,_:=fixture();r.Execute=true;r.Approval="scale:demo/api:2";c.drift=true
    res,err:=r.Run(context.Background())
    if err==nil || res.Status!="manual_review" || c.writes!=1 { t.Fatalf("unsafe concurrent modification: res=%+v err=%v",res,err) }
}

func TestGuardianFailsClosedBeforePlanning(t *testing.T) {
    r,c,planner:=fixture()
    c.d.Replicas=1
    _,err:=r.Run(context.Background())
    if err==nil || planner.calls!=0 || c.writes!=0 { t.Fatal("nonzero deployment should not be modified or planned") }
}

func TestGuardianModelErrorNeverWrites(t *testing.T) {
    r,c,p:=fixture();p.err=errors.New("no Token Factory access");r.Execute=true;r.Approval="scale:demo/api:2"
    _,err:=r.Run(context.Background())
    if err==nil || c.writes!=0 { t.Fatal("planner failure must be safe") }
}

func TestGuardianNameValidation(t *testing.T) {
    for _,name:=range []string{"","-api","api-","API","api;rm -rf /","a/b"} {
        if ValidName(name) { t.Fatalf("accepted invalid name %q",name) }
    }
    for _,name:=range []string{"api","my-app","a2"} {
        if !ValidName(name) { t.Fatalf("rejected valid name %q",name) }
    }
}
