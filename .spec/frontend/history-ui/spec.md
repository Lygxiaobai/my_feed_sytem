---
title: history-ui
status: active
code:
  - frontend/src/history/progress.ts
related:
  - frontend/src/history/rules.ts
  - frontend/src/api/history.ts
  - frontend/src/views/AccountView.vue
  - frontend/src/views/VideoDetailView.vue
  - frontend/src/views/HomeView.vue
  - frontend/src/components/VideoPlayer.vue
---
# history-ui

## raw source
登录用户在「我的」里按未看完和已看完查看浏览历史。从历史打开未看完的详情会回到上次进度；信息流滑动不会按历史进度起播。

## expanded spec
播放器在暂停、切条、心跳（约 10 秒）和页面隐藏时上报进度，不跟 `timeupdate` 打接口。循环回零时用回跳前的进度上报一次完成态。游客只写本机缓存；登录后以服务端为准，读失败时才退回本机。

账号页内容区页签是「作品 / 点赞视频 / 历史」，历史是第三个页签。未看完卡片显示封面进度条和已看到的时刻；已看完不显示进度条。卡片上没有移除或删除。点进详情后，只有服务端（或本机兜底）给出可恢复进度时才 seek，并且等媒体元数据就绪。信息流只采集进度，不按历史 seek。
