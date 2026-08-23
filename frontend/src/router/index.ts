import { createRouter, createWebHistory } from 'vue-router'

import HomeView from '../views/HomeView.vue'
import HotView from '../views/HotView.vue'
import VideoView from '../views/VideoView.vue'
import VideoDetailView from '../views/VideoDetailView.vue'
import AccountView from '../views/AccountView.vue'
import ChangePasswordView from '../views/ChangePasswordView.vue'
import RegisterView from '../views/RegisterView.vue'
import SettingsView from '../views/SettingsView.vue'
import AdminShell from '../views/admin/AdminShell.vue'
import AdminOverviewView from '../views/admin/AdminOverviewView.vue'
import AdminReportsView from '../views/admin/AdminReportsView.vue'
import AdminVideosView from '../views/admin/AdminVideosView.vue'
import AdminUsersView from '../views/admin/AdminUsersView.vue'
import AdminOpsView from '../views/admin/AdminOpsView.vue'
import UserProfileView from '../views/UserProfileView.vue'
import WalletView from '../views/WalletView.vue'
import InvoiceView from '../views/InvoiceView.vue'
import PasswordLoginView from '../views/PasswordLoginView.vue'
import CheckinView from '../views/CheckinView.vue'
import LotteryView from '../views/LotteryView.vue'
import ShareLandingView from '../views/ShareLandingView.vue'
import NotificationsView from '../views/NotificationsView.vue'
import MessagesView from '../views/MessagesView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: HomeView },
    { path: '/feed', redirect: '/' },
    { path: '/likes', name: 'likes', component: HomeView },
    { path: '/following', name: 'following', component: HomeView },
    { path: '/hot', name: 'hot', component: HotView },
    { path: '/video', name: 'video', component: VideoView },
    { path: '/video/:id', name: 'video-detail', component: VideoDetailView, props: true },
    // 分享链接的落地页，解析口令后跳转到真正的详情页。
    { path: '/s/:code', name: 'share-landing', component: ShareLandingView, props: true },
    { path: '/account', name: 'account', component: AccountView },
    { path: '/wallet', name: 'wallet', component: WalletView },
    { path: '/invoice', name: 'invoice', component: InvoiceView },
    { path: '/checkin', name: 'checkin', component: CheckinView },
    { path: '/lottery', name: 'lottery', component: LotteryView },
    { path: '/account/password', name: 'account-password', component: PasswordLoginView },
    { path: '/account/register', name: 'account-register', component: RegisterView },
    { path: '/account/change-password', name: 'account-change-password', component: ChangePasswordView },
    { path: '/settings', name: 'settings', component: SettingsView },
    { path: '/ops', redirect: '/admin/ops' },
    {
      path: '/admin',
      component: AdminShell,
      children: [
        { path: '', name: 'admin', component: AdminOverviewView },
        { path: 'reports', name: 'admin-reports', component: AdminReportsView },
        { path: 'videos', name: 'admin-videos', component: AdminVideosView },
        { path: 'users', name: 'admin-users', component: AdminUsersView },
        { path: 'ops', name: 'admin-ops', component: AdminOpsView },
      ],
    },
    { path: '/u/:id', name: 'user-profile', component: UserProfileView, props: true },
    {
      path: '/notifications',
      name: 'notifications',
      component: NotificationsView,
    },
    {
      path: '/messages',
      name: 'messages',
      component: MessagesView,
    },
  ],
})

export default router
