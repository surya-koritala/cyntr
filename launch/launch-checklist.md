# Cyntr launch checklist

> Pre-launch and launch-day operational checklist. This is the one
> doc you re-read at 4 AM on launch day. Keep it boring, keep it
> sequential, check the boxes.

Target launch slot: **Tuesday, 9:01 AM Pacific (12:01 PM Eastern,
17:01 UTC).** Why Tuesday: HN traffic is highest Tue/Wed; avoid
Monday (people clearing inboxes) and Thursday (people queueing for
Friday). Why 9:01 AM PT: catches the West Coast workday open and
the East Coast lunch break; gives the post the full day to be voted
on.

If this date is moved, the only thing that has to change is the
calendar — every other line below is date-agnostic.

---

## T-7 days: repo polish

- [ ] **README final pass.** First-time visitor read: do they
      understand what Cyntr is in 30 seconds? If not, rewrite the
      first paragraph.
- [ ] **`v1.1.0` release tagged** on GitHub with release notes.
      Release notes link to the federation demo, the blog post, the
      hosted sandbox.
- [ ] **LICENSE file** present at repo root (Apache 2.0). Verify the
      copyright year is 2026.
- [ ] **CODE_OF_CONDUCT.md** present, links to a working email.
- [ ] **CONTRIBUTING.md** present. Verify the test instructions
      actually pass on a clean clone (`go test ./... -race`).
- [ ] **Issue templates** in `.github/ISSUE_TEMPLATE/`: bug, feature,
      deployment-question.
- [ ] **`.github/FUNDING.yml`** — not asked for, but adds polish.
- [ ] **Repo description** (the GitHub "About" sidebar): "Open-source
      AI agent platform — single Go binary, OPA Rego policy,
      federation. Apache 2.0."
- [ ] **Repo topics** (sidebar tags): `ai-agents`, `llm`, `agents`,
      `rego`, `opa`, `policy-as-code`, `multi-tenant`, `go`,
      `apache-2`, `self-hosted`.
- [ ] **Social preview image** uploaded (Settings → Social
      preview). 1280×640 png.
- [ ] **Stars-to-date screenshot saved** — useful "before" image for
      the post-launch blog.

## T-5 days: demo and docs

- [ ] **Federation demo runs cleanly from a fresh clone** on three
      platforms: macOS (arm64), Linux (x86_64), WSL Ubuntu. All
      three exit 0 on `go test ./demos/federation/ -v`.
- [ ] **Federation demo recorded** as a GIF (asciinema → svg, then
      Cleanshot → mp4 → gif). Embedded in README and in the blog
      post.
- [ ] **Demo video script** finalised (`launch/demo-video-script.md`)
      and recorded; 3-minute mp4 uploaded as a GitHub asset and as
      an unlisted YouTube video. Both URLs in the README.
- [ ] **Quick-start tested on a clean machine.** Spin up a fresh
      Ubuntu VM, follow the README exactly, time how long it takes.
      Target: ≤ 5 minutes from `git clone` to a chat response.
- [ ] **docs.cyntr.dev (or /docs subpath) live.** Minimum pages:
      installation, configuration, policy-as-code, federation,
      eval, channel adapters, REST API reference.
- [ ] **Comparison page** (`launch/comparison-page.md`) published at
      cyntr.dev/compare.

## T-3 days: hosted, channels, support

- [ ] **try.cyntr.dev operational.** Free-tier sandbox: mock
      provider, federation demo pre-wired, sample audit data. Rate
      limited per IP. Static-looking enough that someone can poke
      at the dashboard without an account.
- [ ] **Discord server ready.** Channels: `#announcements`, `#general`,
      `#policy-and-rego`, `#federation`, `#help`, `#hosted-sandbox`,
      `#showcase`. Invite link doesn't expire. Pin a "start here"
      message linking to repo + docs.
- [ ] **support@cyntr.dev** alias set up, forwarding to founder
      inbox. Auto-responder: "We see your message and will reply
      within 24h."
- [ ] **issues@cyntr.dev** alias set up, same forwarding.
- [ ] **Status page** at status.cyntr.dev (BetterStack or similar)
      for `try.cyntr.dev` monitoring. Public.
- [ ] **Domain redirects:**
      - cyntr.dev → marketing site
      - try.cyntr.dev → hosted sandbox
      - docs.cyntr.dev → docs
      - discord.cyntr.dev → Discord invite

## T-1 day: rehearsal

- [ ] **Show HN post text frozen.** Final read by someone outside
      the team for "does this sound human." File:
      `launch/show-hn-post.md`.
- [ ] **Twitter thread frozen and scheduled** to draft (do not
      publish from the scheduler — manual at 9:05 AM PT).
- [ ] **LinkedIn post frozen** in drafts.
- [ ] **Reddit posts frozen** in drafts (do NOT submit yet — Reddit
      drafts are subreddit-specific).
- [ ] **Maker comments frozen** for Product Hunt.
- [ ] **Pre-drafted reply templates loaded** into a single text doc
      so you can copy-paste fast.
- [ ] **HN account warmth check.** The account posting the Show HN
      should have > 50 karma and a posting history — HN deprioritises
      brand-new accounts. If the account is fresh, use a more
      established team account.
- [ ] **Notification chaos check.** Mute Slack/Teams for everything
      except the launch channels. Phone on Do Not Disturb except
      starred contacts.
- [ ] **Sleep.** Seriously. 9 AM PT is going to feel early if you
      stay up rehearsing the launch.

## Launch day — Tuesday, by the minute

### 8:50 AM PT — pre-flight

- [ ] Open the Show HN submit page in one tab.
- [ ] Open the Twitter compose window in another tab.
- [ ] Open the LinkedIn post draft in another.
- [ ] Open the Discord announcements channel.
- [ ] Open `try.cyntr.dev` in another tab — verify it's responsive.
- [ ] Open status.cyntr.dev — verify all green.
- [ ] Open the GitHub repo — verify the v1.1.0 release is visible.
- [ ] Run `./demos/federation/run.sh` one more time on a clean
      clone — verify it passes. (If it fails, *stop the launch.*)

### 9:01 AM PT — post Show HN

- [ ] Submit the Show HN post. Title and body from
      `launch/show-hn-post.md`. URL field: the GitHub repo.
- [ ] **Do not edit the post after submitting.** HN's algorithm
      penalises edits in the first hour.
- [ ] Take a screenshot of the submission timestamp.

### 9:03 AM PT — Discord ping

- [ ] Post in `#announcements`: "We just submitted Cyntr to Show HN.
      Link: <HN URL>. We'd love your upvotes (only if you actually
      think it's good — no brigading)."
- [ ] Pin the message.

### 9:05 AM PT — Twitter thread

- [ ] Post the 6-tweet thread from
      `launch/social/twitter-launch-thread.md`. Reply to your own
      first tweet with the HN link.
- [ ] Quote-retweet from any other accounts you control (sparingly).

### 9:15 AM PT — Product Hunt

- [ ] Submit the PH listing from
      `launch/product-hunt-listing.md`. Schedule for tomorrow if PH
      timing matters — same-day double-launches on HN and PH split
      attention.
- [ ] Post Maker comment 1 immediately after launch goes live.

### 9:30 AM PT — LinkedIn

- [ ] Post from `launch/social/linkedin-post.md`. Tag relevant
      colleagues only if they've pre-agreed to be tagged.

### 9:30 AM PT onward — comment triage

- [ ] **Respond to the first 3 comments on Show HN within 60
      minutes.** This is the single highest-leverage action of the
      day for ranking.
- [ ] Use the pre-drafted replies (`show-hn-post.md` section "Three
      top-comment replies") but personalise per comment.
- [ ] Do **not** argue. If someone says something you disagree with,
      either let it sit or post one calm "I see it differently
      because X; happy to keep talking offline." Move on.
- [ ] Watch for the Show HN post hitting the front page (top 30).
      If it does, take a screenshot — useful for the follow-up blog.

### 11:00 AM PT — r/selfhosted

- [ ] Post `launch/social/reddit-r-selfhosted.md`. Drop the follow-up
      comment 30 seconds after submission.

### 12:00 PM PT — r/devops

- [ ] Post `launch/social/reddit-r-devops.md`. Drop the follow-up
      comment 60 seconds after submission.

### 1:00 PM PT — first traffic check

- [ ] Check `try.cyntr.dev` — is it holding up under load? If not,
      raise capacity (you should have a pre-prepared scale-up
      script).
- [ ] Check GitHub stars — write the count down. Useful baseline.
- [ ] Check Discord member count — same.

### 5:00 PM PT — re-engagement

- [ ] Quote-RT the Twitter thread with "still going on HN" if the
      HN post is still on the front page.
- [ ] Post Maker comment 2 on Product Hunt if PH is live.

### 9:00 PM PT — wrap

- [ ] Post Maker comment 3 on PH ("thanks everyone").
- [ ] Final star count screenshot.
- [ ] Final HN comment count.
- [ ] Bed.

---

## Post-launch — week 1

- [ ] **Day 2 (Wednesday):** write the follow-up post — "We launched
      Cyntr on HN yesterday; here's what we learned." Be honest.
      Numbers, surprises, the dumbest thing we got wrong, the
      smartest critique we got. Post on the blog and link from
      Twitter + LinkedIn.
- [ ] **Day 3 (Thursday):** ship one issue from the comments. Pick
      the smallest, most-requested, most-tractable. Tweet "shipped
      v1.1.1 — [one-line summary] — thanks @username for the
      suggestion."
- [ ] **Day 4 (Friday):** schedule Office Hours for the following
      week — public Discord call, "ask us anything about deploying
      Cyntr." Announce in the repo and on Twitter.
- [ ] **Day 7 (next Monday):** weekly star/issue/PR count posted as
      a Tweet. Sets the expectation of progress visibility.

## Things that will go wrong (and the playbook)

- **Federation demo fails on a viewer's machine.** Likely cause:
  port 7700/7800 in use. Reply with: "Try
  `NODE_A_PORT=17700 NODE_B_PORT=17800 ./demos/federation/run.sh`
  to use different ports. Logging this as an issue to surface this
  in the README."
- **try.cyntr.dev goes down.** Status page will say so. Update the
  HN post with a comment, not an edit: "Hosted sandbox is back —
  ran into a rate-limit on our LLM provider, raising it now."
- **Someone finds a security issue.** Reply once: "Thanks for
  catching this. Filing the disclosure privately — please email
  security@cyntr.dev if you have details. We'll patch and credit
  you in the release notes." Don't argue in public.
- **HN flags the post as marketing.** It happens. Don't email dang
  immediately. Wait 4 hours. If the post is still flagged, write
  ONE polite email: "Show HN: Cyntr — we believe this meets the
  Show HN guidelines because [reasons]. Happy to adjust if you
  have specific concerns." Be brief.
- **Hermes/Dify maintainers show up.** Be gracious. They've built
  something great. Don't take potshots. The comparison page is the
  fairest version of our position; if they push back, listen,
  update the page if they're right.
