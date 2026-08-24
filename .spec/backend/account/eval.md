---
scenarios:
  - name: account-registration-login
    description: A new user can register and log in with valid credentials, while invalid credentials are rejected.
    expected: Registration creates one account, valid login establishes an authenticated session, and invalid login does not establish one.
    tags:
      - backend-api
  - name: account-logout
    description: An authenticated user can log out and then access to protected account behavior is rejected.
    expected: The session is invalidated after logout and a later request using the old session is no longer accepted.
    tags:
      - backend-api
  - name: account-password-change
    description: An authenticated user can change the password with the correct old password.
    expected: The old password is rejected after the change, the new password can log in, and the account identity is preserved.
    tags:
      - backend-api
  - name: email-login-registers-once
    description: A new ordinary email requests a code, then verifies the correct six-digit code.
    expected: Mail is sent, the first verify creates an account and session, and a later verify on the same email opens that same account.
    tags:
      - backend-api
  - name: test-mailbox-accepts-any-six-digits
    description: A visitor sends a code to a digits-only address on the test domain and then verifies any six-digit code.
    expected: No mail is sent, any six-digit code succeeds while the send session is valid, and a non-digit or missing session is rejected as an incorrect code.
    tags:
      - backend-api
  - name: new-account-receives-register-gift
    description: A password registration or first email verify creates an account.
    expected: The new account has the registration gift and an existing account opened by a later login does not receive another gift.
    tags:
      - backend-api
  - name: email-code-is-not-distinguished-on-failure
    description: A caller verifies with a wrong code, an expired session, or without sending first.
    expected: Each case returns the same caller-facing incorrect-code outcome and does not reveal whether the address exists.
    tags:
      - backend-api
  - name: account-lookup-is-rate-limited
    description: One client IP looks up accounts by identifier or username faster than the lookup ceiling.
    expected: Later lookups are rejected with the rate-limited outcome and do not reveal whether the remaining names exist.
    tags:
      - backend-api
  - name: passkey-enroll-requires-session
    description: A signed-in user starts and finishes a passkey registration, while a signed-out caller tries the same begin endpoint.
    expected: The authenticated ceremony stores one discoverable credential on that account; the signed-out caller is rejected without creating a credential.
    tags:
      - backend-api
  - name: passkey-login-opens-existing-account
    description: An account that already has a passkey completes a usernameless assertion.
    expected: The service issues a session for that same account and does not create a second account.
    tags:
      - backend-api
  - name: passkey-failure-is-not-distinguished
    description: A caller finishes a passkey login with a missing session, an expired session, or a malformed assertion.
    expected: Each case returns the same caller-facing verification-failed outcome.
    tags:
      - backend-api
  - name: interest-null-reads-as-empty
    description: An account row still has a NULL interests column.
    expected: Reading that account for recommendation, review, or a later tag append treats it as no tags and does not fail.
    tags:
      - backend-api
  - name: interest-tags-fifo
    description: An account records eight distinct interest tags from successive video engagements.
    expected: The stored JSON array keeps the newest seven strings and drops the oldest.
    tags:
      - backend-api
  - name: passkey-can-be-revoked
    description: A signed-in user lists passkeys and deletes one of their own, then another account tries to delete it.
    expected: The owner no longer sees the revoked credential; the other account cannot revoke it.
    tags:
      - backend-api
