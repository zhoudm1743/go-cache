module example

go 1.24.0

require github.com/zhoudm1743/go-cache v0.0.0-00010101000000-000000000000

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/redis/go-redis/v9 v9.11.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	go.etcd.io/bbolt v1.4.2 // indirect
	golang.org/x/sys v0.29.0 // indirect
)

replace github.com/zhoudm1743/go-cache => ../
