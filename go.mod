module mcproxy

go 1.26

require (
    entgo.io/ent v0.14.6 // ORM (codegen)
    github.com/go-chi/chi/v5 v5.2.5 // HTTP router
    github.com/oschwald/geoip2-golang v1.13.0 // GeoIP reader
    modernc.org/sqlite v1.48.2 // SQLite (pure Go)
    github.com/jackc/pgx/v5 v5.9.1 // PostgreSQL driver core
)
