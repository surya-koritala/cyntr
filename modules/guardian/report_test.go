package guardian

import (
    "strings"
    "testing"
)

func TestHTMLReportEscapesUntrustedModelOutput(t *testing.T) {
    dangerous:=`<img src=x onerror=alert(1)>`
    page,err:=HTMLReport(Result{Status:"planned",Before:Deployment{Name:"api",Namespace:"demo",UID:"uid"},Proposal:Proposal{Action:"restore_replicas",DesiredReplicas:1,Explanation:dangerous}})
    if err!=nil { t.Fatal(err) }
    s:=string(page)
    if strings.Contains(s,dangerous) || !strings.Contains(s,"&lt;img") {
        t.Fatal("untrusted model proposal rendered as HTML rather than escaped text")
    }
}
func TestHTMLReportContainsEvidence(t *testing.T) {
    page,err:=HTMLReport(Result{Status:"recovered",Before:Deployment{Name:"api",Namespace:"demo",UID:"uid",Replicas:0},After:Deployment{Name:"api",Namespace:"demo",UID:"uid",Replicas:2,Ready:2}})
    if err!=nil {t.Fatal(err)}
    if !strings.Contains(string(page),"recovered") || !strings.Contains(string(page),"demo / api") {
        t.Fatal("expected real status and workload identity in evidence report")
    }
}
