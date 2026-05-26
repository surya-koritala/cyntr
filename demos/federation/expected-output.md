# Expected output

Running `./run.sh` should produce something close to the following. Field
values that vary per run (`request_id`, `timestamp`) are abbreviated as
`<...>`.

```
=== F9 Federation Demo ===
Cyntr root: /…/cyntr-wt-f9
Demo dir:   /…/cyntr-wt-f9/demos/federation

>> building cyntr binary...
>> binary at /…/cyntr-wt-f9/bin/cyntr

>> starting node-a on :7700
>> starting node-b on :7800
>> waiting for node-a...
>> waiting for node-b...

>> joining peers
   node-a peers: {"data":[{"name":"node-b","endpoint":"http://127.0.0.1:7800",...}],...}
   node-b peers: {"data":[{"name":"node-a","endpoint":"http://127.0.0.1:7700",...}],...}

>> node-a -> node-b: federation.delegate (research -> legal)
{"data":{"peer_id":"node-b","agent":"legal","content":"Default mock response","decision":"allow"},...}

>> node-a -> node-b: federation.delegate (research -> NON-EXISTENT, expect denial)
{"data":null,...,"error":{"code":"DELEGATE_FAILED","message":"remote peer node-b: federation_inbound denied by policy: matched rule \"deny-federated-other\""}}

=== Done. Logs in .run/{node-a,node-b}/cyntr.log ===
```

## What to look for

| Field            | Meaning                                                                 |
| ---------------- | ----------------------------------------------------------------------- |
| `peer_id`        | The node that served the request. `node-b` proves the delegation crossed the federation boundary. |
| `agent`          | Which agent on the target node produced the response.                   |
| `content`        | The agent's reply. With the `mock` provider this is `Default mock response`; with a real provider it would be the actual LLM output. |
| `decision`       | `allow` — node-b's policy explicitly authorised this federation call.   |
| Second call      | Same path, different target agent. node-b's policy refuses it. The error message names the rule that fired (`deny-federated-other`). |

## Verifying without docker

The same code paths are exercised by the in-process Go test:

```bash
go test ./demos/federation/ -v
```

Both `TestFederation_CrossNodeDelegation` and
`TestFederation_PolicyDeniesUnauthorisedAgent` should pass.
