# AWS — IAM MFA Enforcement Policy

Every IAM user with console access must have MFA. The mechanism is a deny
policy attached to all users that revokes everything except MFA management
until the user has registered a device. This runbook is the canonical
implementation.

## 1. The enforcement policy

Create a customer-managed policy `ForceMFA` and attach it to every group that
holds console users. The policy:

- Allows the user to list/get their own user, list MFA devices, and enroll a
  virtual MFA.
- Allows password change.
- Denies everything else when `aws:MultiFactorAuthPresent` is false.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AllowViewAccountInfo",
      "Effect": "Allow",
      "Action": ["iam:ListAccountAliases", "iam:ListUsers", "iam:GetAccountSummary"],
      "Resource": "*"
    },
    {
      "Sid": "AllowManageOwnMFA",
      "Effect": "Allow",
      "Action": [
        "iam:CreateVirtualMFADevice",
        "iam:DeleteVirtualMFADevice",
        "iam:EnableMFADevice",
        "iam:ListMFADevices",
        "iam:ResyncMFADevice"
      ],
      "Resource": [
        "arn:aws:iam::*:mfa/${aws:username}",
        "arn:aws:iam::*:user/${aws:username}"
      ]
    },
    {
      "Sid": "DenyAllExceptUnlessMFAAuthenticated",
      "Effect": "Deny",
      "NotAction": [
        "iam:CreateVirtualMFADevice",
        "iam:EnableMFADevice",
        "iam:GetUser",
        "iam:ListMFADevices",
        "iam:ListVirtualMFADevices",
        "iam:ResyncMFADevice",
        "sts:GetSessionToken"
      ],
      "Resource": "*",
      "Condition": {
        "BoolIfExists": { "aws:MultiFactorAuthPresent": "false" }
      }
    }
  ]
}
```

## 2. Identify users to remediate

```
aws iam list-users --query 'Users[].UserName' --output text | while read u; do
  has_login=$(aws iam get-login-profile --user-name "$u" 2>/dev/null && echo yes)
  mfa_count=$(aws iam list-mfa-devices --user-name "$u" --query 'length(MFADevices)' --output text)
  if [ "$has_login" = "yes" ] && [ "$mfa_count" = "0" ]; then
    echo "NO_MFA: $u"
  fi
done
```

## 3. Onboard one user

1. Operator sends the user the ForceMFA policy ARN and tells them they must
   self-enroll on next login.
2. User logs in, hits the "Security credentials" page, registers a virtual
   MFA device (Authy / 1Password / hardware key).
3. User logs out and back in — the session now has `MultiFactorAuthPresent=true`
   and the deny statement no longer applies.

## 4. Migration plan for an existing account

Don't attach `ForceMFA` to every user on Monday morning. Pattern:

- Week 1: announce the change. Attach ForceMFA to a pilot group (volunteers).
- Week 2: roll out to engineering.
- Week 3: roll out to the rest. Have a break-glass IAM user (with MFA, locked
  in a password manager) for emergencies.

## 5. Continuous enforcement

Add a Config rule `iam-user-mfa-enabled` and an EventBridge rule on
`CreateUser` that auto-attaches `ForceMFA` to the new user's default group.

## 6. Root account

The root account is exempt from IAM policy — its MFA is set separately under
"My Security Credentials" while signed in AS root. Do this once, store the
TOTP secret in the same vault as the break-glass IAM user, and never log in
as root again.
