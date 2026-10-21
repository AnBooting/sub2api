package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func main() {
	email := flag.String("email", "", "Admin email to issue a JWT for (defaults to first active admin)")
	output := flag.String("output", "", "Optional file path for the generated JWT output instead of stdout")
	flag.Parse()

	cfg, err := config.LoadForBootstrap()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	client, sqlDB, err := repository.InitEnt(cfg)
	if err != nil {
		log.Fatalf("failed to init db: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("failed to close db: %v", err)
		}
	}()

	userRepo := repository.NewUserRepository(client, sqlDB)
	authService := service.NewAuthService(client, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user *service.User
	if *email != "" {
		user, err = userRepo.GetByEmail(ctx, *email)
	} else {
		user, err = userRepo.GetFirstAdmin(ctx)
	}
	if err != nil {
		log.Fatalf("failed to resolve admin user: %v", err)
	}
	if user == nil {
		log.Fatalf("admin user not found")
	}

	token, err := authService.GenerateToken(ctx, user)
	if err != nil {
		log.Fatalf("failed to generate token: %v", err)
	}

	if *output != "" {
		if err := os.WriteFile(*output, []byte(renderTokenOutput(user.Email, user.ID, token)), 0o600); err != nil {
			log.Fatalf("failed to write JWT output: %v", err)
		}
		fmt.Fprint(os.Stderr, fileSensitiveWarning(*output))
		return
	}

	fmt.Fprint(os.Stderr, stdoutSensitiveWarning())
	fmt.Print(renderTokenOutput(user.Email, user.ID, token))
}

func renderTokenOutput(email string, id int64, token string) string {
	return fmt.Sprintf("ADMIN_EMAIL=%s\nADMIN_USER_ID=%d\nJWT=%s\n", email, id, token)
}

func stdoutSensitiveWarning() string {
	return "WARNING: the JWT output below is sensitive. Avoid pasting it into logs or CI output; use --output for file mode.\n"
}

func fileSensitiveWarning(path string) string {
	return fmt.Sprintf("WARNING: sensitive JWT output written to %s. Protect this file and delete it after use.\n", path)
}
