"""Extract the current source's original legacy-upgrade scenario for SQLmock replay."""
from pathlib import Path
import sys

repo = Path(sys.argv[1]) / "backend/internal/repository"
body = (repo / "migrations_renamed_replay_integration_test.go").read_text(encoding="utf-8")
body, marker, _ = body.partition("\n\t// 护栏边界：")
if not marker:
    raise SystemExit("Cannot locate original integration scenario boundary; review this head manually")
body += "\n}\n"
body = body.replace("//go:build integration", "//go:build unit", 1)
body = body.replace("TestRenamedMigrationReplayOnLegacyDatabase", "reviewOriginalLegacyIntegrationScenario")
body = body.replace("integrationDB", "reviewLegacyIntegrationDB")
body = body.replace("import (", 'import (\n\t"database/sql"', 1)
body += "\nvar reviewLegacyIntegrationDB *sql.DB\n"
target = repo / "review_original_legacy_scenario_test.go"
if target.exists():
    raise SystemExit(f"Refusing to overwrite {target}")
target.write_text(body, encoding="utf-8")
