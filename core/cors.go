package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	corsSourceDatabase = "database"
	corsSourceDefault  = "default"
	corsSourceEnv      = "env"
)

func DefaultCORSOrigins(appURL string) []string {
	origin, err := originFromURL(appURL)
	if err != nil {
		trimmed := strings.TrimRight(strings.TrimSpace(appURL), "/")
		if trimmed == "" {
			return []string{}
		}
		return []string{trimmed}
	}
	return []string{origin}
}

func normalizeCORSOrigins(input []string, allowWildcard bool) ([]string, error) {
	parts := make([]string, 0, len(input))
	for _, item := range input {
		parts = append(parts, splitCSV(item)...)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		if raw == "*" {
			if !allowWildcard {
				return nil, fmt.Errorf("%w: wildcard CORS origin is not allowed here", ErrValidation)
			}
			if len(parts) != 1 {
				return nil, fmt.Errorf("%w: wildcard CORS origin must be the only origin", ErrValidation)
			}
			return []string{"*"}, nil
		}
		origin, err := normalizeCORSOrigin(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		out = append(out, origin)
	}
	return out, nil
}

func normalizeCORSOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: CORS origin is required", ErrValidation)
	}
	origin, err := originFromURL(raw)
	if err != nil {
		return "", fmt.Errorf("%w: CORS origin is invalid", ErrValidation)
	}
	if origin != strings.TrimRight(raw, "/") {
		return "", fmt.Errorf("%w: CORS origin must not include a path, query, or fragment", ErrValidation)
	}
	return origin, nil
}

func corsWildcard(origins []string) bool {
	return len(origins) == 1 && origins[0] == "*"
}

func EffectiveAdminCORSOrigins(ctx context.Context, pool *pgxpool.Pool, cfg *Config) ([]string, error) {
	if pool == nil {
		return copyStrings(cfg.CORSOrigins), nil
	}
	stored, err := loadStoredInstanceSettings(ctx, pool)
	if err != nil {
		return nil, err
	}
	if stored.CORS.Configured {
		return copyStrings(stored.CORS.AdminOrigins), nil
	}
	return copyStrings(cfg.CORSOrigins), nil
}

func EffectiveProjectCORSOrigins(ctx context.Context, pool *pgxpool.Pool, cfg *Config, slug string) ([]string, error) {
	slug = NormalizeProjectSlug(slug)
	if err := ValidateProjectSlug(slug); err != nil {
		return nil, err
	}
	var origins []string
	var configured bool
	err := pool.QueryRow(ctx, `
		select coalesce(public_cors_origins, '{}'::text[]), public_cors_origins is not null
		from _dbo.projects
		where slug = $1 and disabled_at is null`,
		slug,
	).Scan(&origins, &configured)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrProjectNotFound
		}
		return nil, err
	}
	if configured {
		return copyStrings(origins), nil
	}
	return DefaultCORSOrigins(cfg.AppURL), nil
}

func publicCORSSettings(cfg *Config, stored storedCORSSettings) PublicCORSSettings {
	if stored.Configured {
		return PublicCORSSettings{
			AdminOrigins: copyStrings(stored.AdminOrigins),
			Source:       corsSourceDatabase,
			Wildcard:     corsWildcard(stored.AdminOrigins),
		}
	}
	source := corsSourceDefault
	if cfg.CORSOriginsConfigured {
		source = corsSourceEnv
	}
	return PublicCORSSettings{
		AdminOrigins: copyStrings(cfg.CORSOrigins),
		Source:       source,
		Wildcard:     corsWildcard(cfg.CORSOrigins),
	}
}

func fillProjectCORS(project *Project, cfg *Config) {
	if project == nil {
		return
	}
	if project.CORS.Source != corsSourceDatabase {
		project.CORS = ProjectCORSSettings{
			PublicOrigins: DefaultCORSOrigins(cfg.AppURL),
			Source:        corsSourceDefault,
			Wildcard:      false,
		}
		return
	}
	project.CORS.Wildcard = corsWildcard(project.CORS.PublicOrigins)
}

func fillProjectsCORS(projects []Project, cfg *Config) {
	for i := range projects {
		fillProjectCORS(&projects[i], cfg)
	}
}

func FillProjectCORSForAPI(project *Project, cfg *Config) {
	fillProjectCORS(project, cfg)
}

func FillProjectsCORSForAPI(projects []Project, cfg *Config) {
	fillProjectsCORS(projects, cfg)
}

func copyStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}
