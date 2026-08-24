---
title: account-ui
status: active
code:
  - frontend/src/views/AccountView.vue
related:
  - frontend/src/views/PasswordLoginView.vue
  - frontend/src/views/RegisterView.vue
  - frontend/src/views/ChangePasswordView.vue
  - frontend/src/views/SettingsView.vue
  - frontend/src/views/UserProfileView.vue
  - frontend/src/api/account.ts
  - frontend/src/webauthn.ts
  - frontend/src/stores/auth.ts
  - frontend/src/router/index.ts
  - frontend/src/views/admin/AdminShell.vue
  - frontend/src/views/WalletView.vue
  - frontend/src/views/NotificationsView.vue
  - frontend/src/api/history.ts
  - frontend/src/components/AppShell.vue
---
# account-ui

## raw source
The web application supports email one-time-code login that also registers, optional password login, passkey sign-in, account settings that can enroll and revoke passkeys, password changes, logout, a wallet entry on the signed-in hub, and public profile views.

## expanded spec
Authenticated and unauthenticated routes remain distinct. Successful account mutations update or clear local authentication state as required, while failed requests show an actionable error instead of pretending the mutation succeeded.

The unsigned account surface leads with email and a six-digit code. Sending a code starts a short cooldown on the button. The email field also participates in passkey autofill when the browser offers conditional mediation. A separate control starts an explicit passkey ceremony. WeChat and QQ controls are visible but only explain that they are not available yet. Password login lives on a dedicated route so it is not mixed into the email form. Passkey enrollment and revocation live on the settings page for a signed-in session; a browser or page that is not a secure context explains that passkeys are unavailable instead of starting a ceremony. The dedicated register route sends the visitor back to this email flow instead of collecting a separate username and password. The signed-in hub shows compact 粉丝 / 关注 / 获赞 counts, then a tool row for daily check-in, lottery, wallet, settings, and administration when allowed. Works, liked videos, and history share one tab bar; the current tab is not repeated as a heading. Under 作品, the signed-in hub shows Douyin-style pills for public works, private/unpublished works, and drafts. Work cards themselves have no unpublish, delete, or more control; visibility changes happen on the video from a more sheet, not from share. Browsing history is the third tab, beside 作品 and 点赞视频, and is owned by `history-ui`. That history is split into unfinished and completed videos. The desktop top bar reaches the same wallet and publish pages, the account hub, the notifications inbox, and the private-message dropdown. The recharge control is labeled 充积分. Notifications are owned by `notification-ui`. Private messages are owned by `message-ui`; a public profile that is not the viewer offers 发私信, which opens that surface. A reviewer account sees an administration entry on the signed-in hub; that surface is owned by the admin-ui spec and includes the operations console owned by `ops-ui`. A test-email identity does not open operations or administration. The settings page also hosts the appearance picker owned by `theme-ui`; that control remains available while signed out and does not share account mutation state. Signed-in rename, passkeys, password change, and logout sit in one account card rather than nested cards.
