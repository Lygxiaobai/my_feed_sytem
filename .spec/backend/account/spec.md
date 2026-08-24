---
title: account
status: active
code:
  - backend/internal/account/service.go
  - backend/internal/account/passkey_service.go
related:
  - backend/internal/account/handler.go
  - backend/internal/account/handler_passkey.go
  - backend/internal/account/repo.go
  - backend/internal/account/passkey_repo.go
  - backend/internal/account/passkey.go
  - backend/internal/account/passkey_store.go
  - backend/internal/account/entity.go
  - backend/internal/account/email.go
  - backend/internal/account/otp.go
  - backend/internal/account/smtp.go
  - backend/internal/account/token_cache.go
  - backend/internal/account/review.go
  - backend/internal/account/interests.go
---
# account

## raw source
Accounts support password login, email one-time-code login that also registers, passkey sign-in for accounts that have enrolled a discoverable credential, identity lookup, logout, password changes, and profile changes with authenticated state enforced by the backend. Creating an account also grants a one-time registration coin gift owned by the wallet contract.

## expanded spec
Passwords are not stored as plaintext. Login establishes the service's current authentication state, logout invalidates it, and account mutations must preserve the relationship between identity, token state, and persisted account data. Login, registration, email-code send and verify, passkey login begin and finish, and public identity lookup are rate-limited by client IP so credential stuffing and account enumeration cannot run unbounded.

An account may have no password when it was created by email verification. Email login is login-or-register: a verified address binds to one identity row (`provider=email`) and either opens the existing account or creates one. Account creation and the registration gift succeed or fail together. Ordinary addresses receive a six-digit code by SMTP; the code is stored only as a hash with a short lifetime and is consumed on success. Addresses whose local part is only digits and whose domain is the configured test domain do not send mail and accept any six-digit code after a send session has been opened. Failed verification does not distinguish missing, expired, or wrong codes. WeChat and QQ identities are reserved in the binding table and are not accepted as login providers yet.

Each account stores up to seven interest tags as a JSON string array in a text column. A missing or NULL value means the account has no tags yet and must be read as empty, not as a storage error. Like, tip, and comment append tags taken from the video's stored tag list. A video with no tags leaves interests unchanged. A repeated tag moves to the end. An eighth distinct tag drops the oldest. The field is not returned on public account payloads.

A reviewer may list accounts through administration. That list never includes a password, token, or credential.

Passkeys follow progressive enrollment. A signed-in account may register, list, and revoke discoverable credentials. Registration does not create an account. Sign-in is usernameless: the authenticator returns a user handle that the service resolves to one account, then issues the same JWT as other login methods. Each account may keep a small number of passkeys. A ceremony challenge lives in the session store for a short time and is consumed on use so it cannot be replayed. Failed register or login ceremonies return one caller-facing outcome and do not reveal whether the session, credential, or account was missing. The relying-party ID and origin are taken from the request Origin header, so a credential stays bound to the site the browser presented; an IP address or a non-HTTPS origin other than localhost is rejected.
