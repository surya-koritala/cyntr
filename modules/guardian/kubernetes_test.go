package guardian

import (
    "context"
    "encoding/json"
    "errors"
    "reflect"
    "strings"
    "testing"
)

type recorder struct { args []string; output []byte; err error }
func (r *recorder) Run(ctx context.Context,args ...string)([]byte,error) {
    r.args=append([]string(nil),args...)
    return r.output,r.err
}
func TestKubernetesRead(t *testing.T) {
    rec:=&recorder{output:[]byte(`{"metadata":{"uid":"abc","name":"api","namespace":"demo"},"spec":{"replicas":0},"status":{"readyReplicas":0}}`)}
    k:=Kubernetes{Context:"kind-guardian",Runner:rec}
    d,err:=k.Read(context.Background(),"demo","api")
    if err!=nil || d.UID!="abc" || d.Ready!=0 { t.Fatalf("read returned %+v err=%v",d,err) }
    if !reflect.DeepEqual(rec.args,[]string{"--context","kind-guardian","--request-timeout=15s","-n","demo","get","deployment","api","-o","json"}) {
        t.Fatalf("unexpected exec args: %#v",rec.args)
    }
}
func TestKubernetesPatchHasAtomicPreconditions(t *testing.T) {
    rec:=&recorder{}
    k:=Kubernetes{Context:"kind-guardian",Runner:rec}
    if err:=k.Scale(context.Background(),"demo","api","uid-1",0,2);err!=nil { t.Fatal(err) }
    if len(rec.args)!=10 || rec.args[6]!="deployment" || rec.args[7]!="api" || rec.args[8]!="--type=json" {
        t.Fatalf("unrecognized kubectl args: %#v",rec.args)
    }
    var patch []struct { Op string `json:"op"`; Path string `json:"path"`; Value any `json:"value"` }
    if !strings.HasPrefix(rec.args[9],"-p") { t.Fatal("missing patch") }
    if err:=json.Unmarshal([]byte(rec.args[9]),&patch); err==nil {
        t.Fatal("patch should be passed as two distinct arguments")
    }
}
func TestKubernetesRejectsUnsafeInput(t *testing.T) {
    rec:=&recorder{}
    k:=Kubernetes{Context:"-bad",Runner:rec}
    if _,err:=k.Read(context.Background(),"demo","api");err==nil { t.Fatal("unsafe context accepted") }
    k.Context="kind-demo"
    if err:=k.Scale(context.Background(),"demo;whoami","api","uid",0,1);err==nil { t.Fatal("injected namespace accepted") }
    if err:=k.Scale(context.Background(),"demo","api","uid",0,15);err==nil { t.Fatal("unbounded replica change accepted") }
}
func TestKubernetesFailsClosedOnMissingReplicas(t *testing.T) {
    rec:=&recorder{output:[]byte(`{"metadata":{"uid":"abc","name":"api","namespace":"demo"},"spec":{},"status":{}}`)}
    if _,err:=(Kubernetes{Context:"kind-demo",Runner:rec}).Read(context.Background(),"demo","api");err==nil {
        t.Fatal("missing replica field must fail")
    }
}
func TestKubernetesPropagatesExecutionFailure(t *testing.T) {
    rec:=&recorder{err:errors.New("permission denied")}
    if err:=(Kubernetes{Context:"kind-demo",Runner:rec}).Scale(context.Background(),"demo","api","u",0,1);err==nil {
        t.Fatal("write errors must propagate")
    }
}
