package guardian

import (
    "bytes"
    "html/template"
    "time"
)

// HTMLReport renders a portable incident-evidence page. All dynamic content
// is HTML-escaped by html/template; it does not load remote assets.
func HTMLReport(result Result) ([]byte,error) {
    view:=struct{
        Result Result
        Generated string
        HasAfter bool
    }{Result:result,Generated:time.Now().UTC().Format(time.RFC3339),HasAfter:result.After.UID!=""}
    t,err:=template.New("incident").Parse(reportTemplate)
    if err!=nil{return nil,err}
    var out bytes.Buffer
    if err:=t.Execute(&out,view);err!=nil{return nil,err}
    return out.Bytes(),nil
}

const reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Cyntr Guardian — Incident Evidence</title>
<style>
:root{color-scheme:dark;font-family:Inter,system-ui,-apple-system,sans-serif}
*{box-sizing:border-box}
body{background:#0a101e;color:#eaf0ff;margin:0;padding:24px}
main{max-width:980px;margin:0 auto}
small,.sub{color:#91a1bd}
header{display:flex;justify-content:space-between;align-items:center;gap:14px;flex-wrap:wrap;border-bottom:1px solid #28364c;padding-bottom:20px}
h1{font-size:28px;margin:6px 0}h2{font-size:17px;margin:0 0 18px}
.tag{border:1px solid #6c829c;border-radius:20px;padding:7px 14px;font-weight:700;text-transform:uppercase;letter-spacing:.06em;font-size:12px}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(250px,1fr));gap:16px;margin:22px 0}
.card{background:#131f32;border:1px solid #2d415e;border-radius:14px;padding:22px;min-width:0}
.stat{font-size:36px;font-weight:760;line-height:1.2}
ul{padding-left:23px}li{margin:12px 0;overflow-wrap:anywhere}
code{font-family:ui-monospace,monospace;font-size:.9em;overflow-wrap:anywhere}
p{line-height:1.6;overflow-wrap:anywhere}
.timeline{list-style:none;padding:0;margin:0;border-left:2px solid #56739a}
.timeline li{position:relative;margin:0;padding:0 0 19px 23px}
.timeline li:before{content:"";position:absolute;left:-7px;top:4px;background:#54acff;width:12px;height:12px;border-radius:50%}
.phase{font-weight:700;text-transform:uppercase;letter-spacing:.06em;font-size:11px;color:#71c6ff}
footer{margin-top:25px;padding:16px 0;border-top:1px solid #28364c}
</style>
</head>
<body><main>
<header><div><small>CYNTR / GUARDIAN · NEBIUS × NVIDIA</small>
<h1>Incident evidence</h1><div class="sub">Report generated {{.Generated}}</div></div>
<div class="tag">{{.Result.Status}}</div></header>
<p class="sub">Execution time: <b>{{.Result.DurationMs}} ms</b> (including inference and verification)</p>
<div class="cards">
<section class="card"><h2>Deployment</h2>
<div><code>{{.Result.Before.Namespace}} / {{.Result.Before.Name}}</code></div>
<p class="sub">UID: <code>{{.Result.Before.UID}}</code></p></section>
<section class="card"><h2>Before</h2>
<div class="stat">{{.Result.Before.Ready}} / {{.Result.Before.Replicas}}</div><div class="sub">ready / desired replicas</div></section>
<section class="card"><h2>After</h2>
<div class="stat">{{.Result.After.Ready}} / {{.Result.After.Replicas}}</div><div class="sub">ready / desired replicas</div></section>
</div>
<section class="card"><h2>NVIDIA model proposal</h2>
<p><b>Action:</b> <code>{{.Result.Proposal.Action}}</code></p>
<p><b>Target replicas:</b> {{.Result.Proposal.DesiredReplicas}}</p>
<p><b>Rationale:</b> {{.Result.Proposal.Explanation}}</p>
{{if .Result.Proposal.Provider}}<p><b>Provider:</b> {{.Result.Proposal.Provider}}</p>{{end}}
{{if .Result.Proposal.Model}}<p><b>Model:</b> <code>{{.Result.Proposal.Model}}</code></p>{{end}}
{{if .Result.Proposal.Provider}}<p><b>Token usage:</b> {{.Result.Proposal.InputTokens}} input / {{.Result.Proposal.OutputTokens}} output</p>{{end}}
{{if .Result.ApprovalRequired}}<p><b>Required approval:</b> <code>{{.Result.ApprovalRequired}}</code></p>{{end}}
{{if .Result.Error}}<p><b>Execution error:</b> {{.Result.Error}}</p>{{end}}
</section>
<section class="card" style="margin-top:20px"><h2>Auditable event timeline</h2>
<ol class="timeline">
{{range .Result.Events}}
<li><div class="phase">{{.Phase}}</div>
<small>{{.Time.Format "2006-01-02 15:04:05 UTC"}}</small><p>{{.Detail}}</p></li>
{{end}}
</ol></section>
<footer class="sub">Generated directly from the Guardian run result; no external telemetry or metrics are implied. Confirm live Nebius integration independently.</footer>
</main></body></html>`;
