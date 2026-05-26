# AWS — S3 Bucket Public Access Remediation

If an audit flags an S3 bucket as public, treat it as a P0 until you've
classified the data. "Public bucket of marketing PDFs" is fine; "public bucket
of customer exports" is a breach. The remediation below moves a bucket from
"public" to "private" without breaking legitimate access.

## 1. Confirm what "public" means for this bucket

Three independent mechanisms can make a bucket public:

- **Bucket ACL** — `AllUsers` or `AuthenticatedUsers` grant.
- **Bucket policy** — a `Principal: "*"` statement with `s3:GetObject`.
- **Public Access Block** disabled — the safety net.

Run:

```
aws s3api get-bucket-acl --bucket <name>
aws s3api get-bucket-policy --bucket <name>
aws s3api get-public-access-block --bucket <name>
aws s3api get-bucket-policy-status --bucket <name>
```

The last call returns `IsPublic: true/false` — that's the authoritative answer.

## 2. Identify legitimate access patterns BEFORE locking down

Enable S3 server access logging or CloudTrail data events for the bucket for
24 hours. Public bucket policies usually exist because something genuinely
needs anonymous read (a static site, a CDN origin without OAC, public assets).
Locking the bucket cold breaks that flow.

Document:

- Which CIDRs / user agents access the bucket.
- Which prefixes are hit.

## 3. Enable Block Public Access at the bucket level

```
aws s3api put-public-access-block \
  --bucket <name> \
  --public-access-block-configuration \
    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
```

This is reversible and immediate. If something breaks, you'll know within
minutes.

## 4. Replace anonymous access with scoped access

If the bucket genuinely needs internet access:

- Front it with CloudFront and use **Origin Access Control (OAC)** — the
  bucket stays private, CloudFront's signed identity reads from it.
- Or move the public-read prefix to a new bucket explicitly carved out for
  public assets, with a clear naming convention (`public-assets-*`).

## 5. Enable account-level Block Public Access

```
aws s3control put-public-access-block \
  --account-id <id> \
  --public-access-block-configuration \
    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
```

This is the long-term fix — even if a future user removes the per-bucket
block, the account-level setting catches it.

## 6. Audit the policy graveyard

Search for buckets with `*` principal policies:

```
aws s3api list-buckets --query "Buckets[].Name" --output text | \
  xargs -n1 -I{} aws s3api get-bucket-policy --bucket {} 2>/dev/null
```

Most "public" buckets are accidents from 2018 that nobody cleaned up.
