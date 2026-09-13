package routes

import (
	"sort"
	"sync"
)

// Strapper 接收 any，与具体框架解耦
type Strapper interface {
	Strap(r any)
}

type strapFunc func(r any)

func (f strapFunc) Strap(r any) { f(r) }

// StrapFunc 泛型辅助：让业务包写有类型的函数，内部自动处理断言
// 类型断言发生在已经依赖框架的业务包里
func StrapFunc[R any](fn func(R)) Strapper {
	return strapFunc(func(r any) {
		fn(r.(R))
	})
}

var (
	mu     sync.RWMutex
	straps = make(map[string]Strapper)
	ronce  sync.Once
)

// Register 线程安全，供各业务包 init() 调用
func Register(name string, sf Strapper) {
	mu.Lock()
	defer mu.Unlock()
	straps[name] = sf
}

// Routers 统一挂载，r 传入具体的路由器实例
func Routers(r any, names ...string) {
	ronce.Do(func() {
		mu.RLock()
		defer mu.RUnlock()

		keys := names
		if len(keys) == 0 {
			for name := range straps {
				keys = append(keys, name)
			}
			sort.Strings(keys)
		}

		logger().Info("Routers", "names", keys)
		for _, name := range keys {
			if sf, ok := straps[name]; ok {
				logger().Info("start router for ", "name", name)
				sf.Strap(r)
			} else {
				logger().Warn("strap not found", "name", name)
			}
		}
	})
}
