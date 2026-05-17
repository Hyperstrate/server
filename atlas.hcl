data "external_schema" "gorm_sqlite" {
  program = ["go", "run", "./cmd/migrate", "--dialect", "sqlite"]
}

data "external_schema" "gorm_postgres" {
  program = ["go", "run", "./cmd/migrate", "--dialect", "postgres"]
}

env "local" {
  src = data.external_schema.gorm_sqlite.url
  url = "sqlite://hyperstrate-dev.db?_fk=1"
  dev = "sqlite://dev?mode=memory&_fk=1"
  migration {
    dir    = "file://internal/db/migrations/sqlite"
    format = atlas
  }
  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}

env "production" {
  src = data.external_schema.gorm_postgres.url
  # Set DATABASE_URL when running: atlas migrate apply --env production
  url = getenv("DATABASE_URL")
  # Use a throwaway Postgres container: docker run --rm -p 5432:5432 -e POSTGRES_PASSWORD=dev postgres:16
  dev = getenv("DATABASE_DEV_URL")
  migration {
    dir    = "file://internal/db/migrations/postgres"
    format = atlas
  }
}
