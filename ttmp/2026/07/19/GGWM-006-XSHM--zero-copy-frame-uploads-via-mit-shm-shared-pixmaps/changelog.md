# Changelog

## 2026-07-19

- Initial workspace created


## 2026-07-19

Implemented MIT-SHM shared-pixmap uploads (pkg/xshm + paintFrame integration with PutImage fallback and GO_GO_WM_NO_SHM kill-switch). Pixel-identical vs fallback; zero leaked segments incl. kill -9 (IPC_RMID idiom; background-pixmap reference reset before destroy). Workspace creation 76→55ms CPU; boot to 9 workspaces 0.38s; drag-stress profile now 53% pure conversion.

### Related Files

- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/wmx11/manage.go — upload branch
- /home/manuel/workspaces/2026-07-18/go-go-wm/go-go-wm/pkg/xshm/xshm.go — the shared-memory surface

