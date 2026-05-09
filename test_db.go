package main

import (
    "ec2-api/config"
    "ec2-api/internal/infra/database"
    "ec2-api/internal/infra/repository"
    "fmt"
)

func main() {
    cfg, _ := config.Load()
    postgresConn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
        cfg.DB.User, cfg.DB.Password, cfg.DB.Host, cfg.DB.Port, cfg.DB.Database, cfg.DB.SSLMode)
    db, err := database.NewPostgresDB(postgresConn)
    if err != nil { panic(err) }
    repo := repository.NewHostRepository(db.DB)
    hosts, err := repo.GetBestHosts(10)
    if err != nil { panic(err) }
    fmt.Printf("Best hosts: %d\n", len(hosts))
}
