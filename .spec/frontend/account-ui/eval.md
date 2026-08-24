---
scenarios:
  - name: account-login
    description: A user can register or log in and reach an authenticated account surface.
    expected: Valid credentials establish the session and invalid credentials show a visible failure without entering an authenticated state.
    tags:
      - frontend-e2e
      - desktop
  - name: account-email-login
    description: A visitor enters an email, requests a code, and submits a six-digit code.
    expected: A successful verify stores the session and shows the authenticated account surface; a failed verify stays signed out and shows the server message.
    tags:
      - frontend-e2e
      - desktop
  - name: account-oauth-placeholder
    description: A visitor clicks WeChat or QQ login.
    expected: No session is created and the visitor is told the method is not available yet.
    tags:
      - frontend-e2e
      - desktop
  - name: account-passkey-login
    description: A visitor with an enrolled passkey signs in from the unsigned account surface.
    expected: A successful assertion stores the session and shows the authenticated account surface; a cancelled or failed ceremony stays signed out.
    tags:
      - frontend-e2e
      - desktop
  - name: account-passkey-settings
    description: A signed-in user adds a passkey on the settings page and later removes it.
    expected: The new passkey appears in the list after enrollment and disappears after deletion; a page that is not a secure context does not start the authenticator prompt.
    tags:
      - frontend-e2e
      - desktop
  - name: account-password-login-page
    description: A visitor opens password login from the unsigned account surface.
    expected: The password form is on its own page and is not mixed with the email code fields; a successful login returns to the authenticated hub.
    tags:
      - frontend-e2e
      - desktop
  - name: account-wallet-entry
    description: A signed-in user opens the wallet from the account hub.
    expected: The wallet page loads for that session and a signed-out visitor cannot stay on the wallet.
    tags:
      - frontend-e2e
      - desktop
  - name: topbar-utility-entries
    description: A user uses the desktop top-bar entries aligned with search.
    expected: The recharge entry is labeled 充积分 and opens the wallet; publish and the avatar open the existing publish and account pages; the notifications bell opens the inbox; messages opens the private-chat dropdown; client and wallpaper entries are absent; a signed-out click on an authenticated entry goes to the account page.
    tags:
      - frontend-e2e
      - desktop
  - name: account-works-privacy-filters
    description: A signed-in author opens the works tab on the account hub, then switches to private works and drafts.
    expected: Public works, unpublished works, and drafts sit in separate pills under 作品. Cards have no more, unpublish, or delete control. An unpublished work appears only under 私密作品.
    tags:
      - frontend-e2e
      - desktop
      - mobile
  - name: account-history-entry
    description: A signed-in user opens browsing history from the account hub.
    expected: The works card tabs are 作品, 点赞视频, and 历史. Unfinished and completed lists are separate, unfinished cards show resume progress, and a completed card does not offer a leftover seek position.
    tags:
      - frontend-e2e
      - desktop
  - name: account-admin-entry
    description: A signed-in reviewer looks at the account hub; a signed-in non-reviewer does the same.
    expected: Only the reviewer sees an administration entry that opens the workbench owned by admin-ui, including operations. A test-email account without reviewer access sees neither administration nor a separate operations entry.
    tags:
      - frontend-e2e
      - desktop
  - name: account-logout
    description: A logged-in user can log out and cannot continue using authenticated-only views.
    expected: Local authentication state is cleared and protected navigation requires login again.
    tags:
      - frontend-e2e
      - desktop
