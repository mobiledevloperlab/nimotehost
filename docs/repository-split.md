# 独立仓库与源码来源

Nimote Host 从 `nimote-connect` 的 `host/` 提取为同级独立仓库。
提取基线：Connect 提交 `4701d14`（fix(host): support agent model controls and native history discovery）。
新仓库不继承 Connect 的 Git 历史；此前实现可在 Connect 的该提交及其父提交追溯。
本地初始化未创建或推送远端仓库。

Host 独立维护 Go 模块、CI、Dependabot、测试、源码导出、分发构建与许可清单。
不要求 Connect 或其他同级仓库存在，不使用跨仓库 Go replace、workspace 或 submodule。
后续 Host 修改只在这里进行，不再向 Connect 回灌或维护另一份源码。

Host 专属验收、待办和证据已迁入 docs；历史路径与旧提交号保留其原始含义。
综合产品审计、旧连接服务和网络传输资料留在 Connect，跨仓库引用使用仓库 URL。
源码导出仍按白名单排除 Git 元数据、二进制、构建缓存及本机配置。

源码目录移动不影响用户级安装路径与已运行服务，也不改变 Host 身份和会话记录。
