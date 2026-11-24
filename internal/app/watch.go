package app

import (
	"context"
	"time"

	"daily-agent/internal/store"
)

// DataChangedEvent — событие окна: базу изменил другой процесс, например
// свой MCP-сервер или внешний агент. Экраны перечитывают данные.
const DataChangedEvent = "data:changed"

// DataChanged — данные события изменения базы.
type DataChanged struct{}

// WatchInterval — как часто ядро проверяет признак изменений базы.
var WatchInterval = time.Second

// WatchData раз в WatchInterval проверяет признак изменений базы и вызывает
// changed, когда в неё записал другой процесс. Работает, пока не отменён ctx.
func WatchData(ctx context.Context, st *store.Store, changed func()) {
	last, err := st.DataVersion(ctx)
	if err != nil {
		return
	}
	tick := time.NewTicker(WatchInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			v, err := st.DataVersion(ctx)
			if err != nil || v == last {
				continue
			}
			last = v
			changed()
		}
	}
}
