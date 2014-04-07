Sample redis config

Every entry is a hash, which must contain at least an id field. This field is
for absolute versioning, for when multiple servers disagree on which has the
correct value. We can implement a monotonic counter for ids later on, or just
realy on UnixNano being "good enough".

dev/web/apiary
    id: 123456789 
    version: registery.w.n/apiary:20140101.1
    environment: `{"DATABASE_URL": "postgres://...",
                   "PORT": "6000"}`
dev/web/hosts/i-abc1234/apiary
    id: 123456789
    location: `{"EXTERNAL_IP": "10.0.1.2",
                "EXTERNAL_PORT": 6000,
                "INTERNAL_IP": "172.0.1.2",
                "INTERNAL_PORT": 49123}`

dev/worker/honeycomb
    id: 123456789
    version: registry.w.n/honeycomb:20141212.1
    environment: `{"DATABASE_URL": "postgres://...",
                   "PORT = 7000"}`
dev/worker/grus
    id: 123456789
    version = registry.w.n/grus:20141212.1
    environment = `{"DATABASE_URL": "postgres://...",
                    "THREADS": "5",
                    "PORT", "8000"}`
dev/worker/hosts/i-xyz1234/honeycomb/
    id: 123456789
    location: `{"EXTERNAL_IP": "10.0.1.5",
                "EXTERNAL_PORT": 7000,
				"INTERNAL_IP": "172.0.1.4",
				"INTERNAL_PORT": 34023}`
dev/worker/hosts/i-xyz1234/grus/
    id: 1234567889
    location: `{"EXTERNAL_IP": "10.0.1.5",
                "EXTERNAL_PORT", 8000,
                "INTERNAL_IP": "172.0.1.23",
                "INTERNAL_PORT": 21235}`
prod/web/...
