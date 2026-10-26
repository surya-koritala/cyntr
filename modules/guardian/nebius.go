package guardian

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"
    "time"
)

// NebiusPlanner sends real runtime requests to NVIDIA Nemotron through
// Nebius Token Factory. Its output is untrusted until Runner validates it.
type NebiusPlanner struct {
    APIKey string
    BaseURL string
    Model string
    Client *http.Client
}

const DefaultNebiusBaseURL = "https://api.tokenfactory.nebius.com/v1"
const DefaultNebiusModel = "nvidia/nemotron-3-super-120b-a12b"

func (p NebiusPlanner) Propose(ctx context.Context, incident Incident) (Proposal, error) {
    if strings.TrimSpace(p.APIKey) == "" { return Proposal{}, errors.New("NEBIUS_API_KEY required") }
    base := p.BaseURL
    if base == "" { base = DefaultNebiusBaseURL }
    u, err := url.Parse(base)
    if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
        return Proposal{}, errors.New("Nebius base URL must be HTTPS (HTTP permitted only for local tests)")
    }
    if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v1") {
        return Proposal{}, errors.New("Nebius base URL must end with /v1")
    }
    model := p.Model
    if model == "" { model = DefaultNebiusModel }
    // Never ask the model to construct commands or grant itself access.
    systemPrompt := "You are Cyntr Guardian incident planner, not an executor. Output ONLY a JSON object with keys action, desired_replicas, explanation. Allowed action is restore_replicas when the deployment has 0 desired replicas AND 0 ready replicas and symptoms indicate a service outage; otherwise action must be none with desired_replicas 0. If restoring, choose only 1, 2 or 3 replicas. Do not output shell commands, YAML, markdown, or secrets. Mention uncertain evidence in explanation."
    incidentJSON, _ := json.Marshal(incident)
    body, err := json.Marshal(map[string]any{
        "model":model, "temperature":0,
        "messages":[]map[string]string{
            {"role":"system","content":systemPrompt},
            {"role":"user","content":string(incidentJSON)},
        },
    })
    if err != nil { return Proposal{}, err }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base,"/")+"/chat/completions", bytes.NewReader(body))
    if err != nil { return Proposal{}, err }
    req.Header.Set("Authorization","Bearer "+p.APIKey)
    req.Header.Set("Content-Type","application/json")
    client := p.Client
    if client == nil { client=&http.Client{Timeout:45*time.Second} }
    resp, err := client.Do(req)
    if err != nil { return Proposal{}, fmt.Errorf("Token Factory call: %w",err) }
    defer resp.Body.Close()
    if resp.StatusCode != 200 {
        // No arbitrary remote error bodies in logs: some providers echo inputs.
        return Proposal{}, fmt.Errorf("Token Factory returned HTTP %d",resp.StatusCode)
    }
    var response struct {
        Choices []struct {
            Message struct { Content string `json:"content"` } `json:"message"`
        } `json:"choices"`
        Model string `json:"model"`
        Usage struct {
            Prompt int `json:"prompt_tokens"`
            Completion int `json:"completion_tokens"`
        } `json:"usage"`
    }
    limited := io.LimitReader(resp.Body, 256*1024)
    if err := json.NewDecoder(limited).Decode(&response); err != nil { return Proposal{}, fmt.Errorf("decode Token Factory response: %w",err) }
    if len(response.Choices)==0 { return Proposal{}, errors.New("Token Factory returned no choices") }
    raw := strings.TrimSpace(response.Choices[0].Message.Content)
    // Some models wrap JSON with code fences. Nothing outside a strict JSON
    // object is accepted; this only removes one enclosing markdown fence.
    if strings.HasPrefix(raw, "```") {
        firstNewline := strings.IndexByte(raw, '\n')
        if firstNewline < 0 || !strings.HasSuffix(raw,"```") {
            return Proposal{}, errors.New("unexpected model output")
        }
        raw = strings.TrimSpace(strings.TrimSuffix(raw[firstNewline+1:], "```"))
    }
    var parsed struct {
        Action string `json:"action"`
        DesiredReplicas int32 `json:"desired_replicas"`
        Explanation string `json:"explanation"`
    }
    d:=json.NewDecoder(strings.NewReader(raw))
    d.DisallowUnknownFields()
    if err:=d.Decode(&parsed);err!=nil { return Proposal{}, fmt.Errorf("unparseable model plan: %w",err) }
    if err:=d.Decode(new(any));err!=io.EOF { return Proposal{}, errors.New("model returned trailing data") }
    if parsed.Explanation=="" || len(parsed.Explanation)>3000 {
        return Proposal{}, errors.New("model explanation missing or too long")
    }
    return Proposal{Action:parsed.Action,DesiredReplicas:parsed.DesiredReplicas,
        Explanation:parsed.Explanation,Provider:"nebius-token-factory",Model:model,
        InputTokens:response.Usage.Prompt,OutputTokens:response.Usage.Completion},nil
}
