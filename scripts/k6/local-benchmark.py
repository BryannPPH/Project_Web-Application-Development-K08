"""Prepare and run the k6 workload on the local Milestone 2 Compose stack."""

import argparse
from collections import Counter, defaultdict
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import gzip
import hashlib
from http.cookiejar import CookieJar
import json
import os
from pathlib import Path
import secrets
from statistics import median, quantiles
import subprocess
from urllib.error import HTTPError
from urllib.request import (
    HTTPCookieProcessor, HTTPRedirectHandler, ProxyHandler, Request, build_opener,
)


ROOT = Path(__file__).resolve().parents[2]
COMPOSE = ("docker", "compose", "--env-file", ".env.test", "-p", "wbd-k08-m2test")
IMAGE = "grafana/k6:0.56.0"
SCRIPT = ROOT / "scripts/k6/migration-queue.js"
BASELINE_VUS = 1
STAGES = {
    "baseline_1_vu": 60, "ramp_to_10_vu": 120, "peak_30_vu": 180,
    "ramp_down_to_10_vu": 120, "ramp_to_0_vu": 60, "drain": None,
}


def utc_now():
    return datetime.now(timezone.utc).isoformat()


def docker(*args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, text=True, **kwargs)


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class App:
    def __init__(self):
        # Read the effective frontend port from this fixed Compose project.
        published = docker(*COMPOSE, "port", "frontend", "80", capture_output=True).stdout
        port = int(published.splitlines()[0].rsplit(":", 1)[1])
        self.origin = f"http://localhost:{port}"
        self.cookies = CookieJar()
        self.opener = build_opener(ProxyHandler({}), NoRedirect(), HTTPCookieProcessor(self.cookies))
        self.csrf = ""
        self.login()

    def login(self):
        login = self.call("POST", "/auth/login", {
            "username": os.environ["BOOTSTRAP_ADMIN_USERNAME"],
            "password": os.environ["BOOTSTRAP_ADMIN_PASSWORD"],
        })
        self.csrf = login["csrfToken"]
        self.user_id = login["user"]["id"]

    def call(self, method, path, data=None):
        payload = None if data is None else json.dumps(data).encode()
        request = Request(self.origin + "/api/v1" + path, data=payload, method=method, headers={
            "Origin": self.origin, "X-CSRF-Token": self.csrf, "Content-Type": "application/json",
        })
        try:
            with self.opener.open(request, timeout=30) as response:
                return json.loads(response.read())
        except HTTPError as exc:
            try:
                error = json.load(exc).get("error", {})
            except (ValueError, AttributeError):
                error = {}
            raise RuntimeError(f"{method} {path}: HTTP {exc.code} {error.get('code', '')}") from None


def prepare(app, folder, smoke):
    lane_count, per_lane = (2, 2) if smoke else (30, 333)
    prefix = folder.name
    names = [f"{prefix}_{lane:02}" for lane in range(1, lane_count + 1)]
    docker(*COMPOSE, "exec", "-T", "target-db", "sh", "-ec",
           'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres',
           input="".join(f"CREATE DATABASE {name};\n" for name in names))

    group = app.call("POST", "/admin/groups", {"name": prefix})["group"]["id"]

    def register_database(name):
        docker(*COMPOSE, "exec", "-T", "target-db", "sh", "-ec",
               'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1"', "sh", name,
               input="CREATE TABLE k6_rows (id bigint PRIMARY KEY);\n")
        db = app.call("POST", "/admin/databases", {
            "name": name, "engine": "postgresql", "host": "target-db", "port": 5432,
            "databaseName": name, "username": os.environ["TARGET_DB_USER"],
            "password": os.environ["TARGET_DB_PASSWORD"], "sslMode": "disable",
        })["database"]["id"]
        app.call("PUT", f"/admin/groups/{group}/databases/{db}", {"accessLevel": "view_edit"})
        return name, db

    def create_lane(database):
        name, db = database
        path = f"/databases/{db}/migrations"
        cases = []
        for row in range(1, per_lane + 1):
            migration = app.call("POST", path, {
                "name": f"k6 insert row {row}",
                "sql": f"INSERT INTO k6_rows (id) VALUES ({row});",
                "downSql": f"DELETE FROM k6_rows WHERE id = {row};",
            })
            migration_id = migration["id"]
            review = app.call("POST", f"{path}/{migration_id}/approvals", {
                "action": "apply", "revision": migration["revision"], "decision": "approved",
            })["review"]
            if review["status"] != "approved" or review["requiredCount"] != 1:
                raise RuntimeError(f"Approval belum lengkap untuk {name}, migration {row}.")
            case = {"databaseId": db, "migrationId": migration_id, "action": "apply"}
            if cases:
                case["predecessorMigrationId"] = cases[-1]["migrationId"]
            cases.append(case)
        print(f"{name}: {len(cases)} migration approved", flush=True)
        return cases

    with ThreadPoolExecutor(max_workers=6) as pool:
        databases = list(pool.map(register_database, names))
        # Granting access revokes existing sessions for the affected member.
        app.call("POST", f"/admin/groups/{group}/members/{app.user_id}")
        app.login()
        app.call("PUT", f"/admin/groups/{group}/leader", {"userId": app.user_id})
        lanes = list(pool.map(create_lane, databases))
    # Round robin so the beginning of the run already covers different databases.
    manifest = [case for level in zip(*lanes) for case in level]
    write_json(folder / "cases.json", manifest)
    write_json(folder / "fixture.json", {
        "databaseCount": lane_count, "caseCount": len(manifest), "smoke": smoke,
    })
    print(f"Manifest siap: {folder.relative_to(ROOT)}/cases.json")


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def summarize(samples, fixture):
    tracked = {
        "api_latency_ms", "api_error_rate", "api_requests", "queue_wait_ms",
        "job_duration_ms", "migration_jobs_accepted", "migration_jobs_completed",
        "migration_case_manifest_exhausted",
    }
    stages = {name: defaultdict(list) for name in ({"smoke": None} if fixture["smoke"] else STAGES)}
    statuses = Counter()
    with gzip.open(samples, "rt", encoding="utf-8") as stream:
        for line in stream:
            sample = json.loads(line)
            if sample["type"] != "Point" or sample["metric"] not in tracked:
                continue
            point = sample["data"]
            tags = point["tags"]
            if tags["stage"] not in stages:
                continue
            stages[tags["stage"]][sample["metric"]].append(point["value"])
            if sample["metric"] == "migration_jobs_completed":
                statuses[tags["status"]] += 1

    def p95(values):
        if len(values) < 2:
            return values[0] if values else None
        return quantiles(values, n=100, method="inclusive")[94]

    stage_results = {}
    for name, metrics in stages.items():
        seconds = STAGES.get(name)
        api_latency = metrics["api_latency_ms"]
        api_errors = metrics["api_error_rate"]
        requests = int(sum(metrics["api_requests"]))
        completed = int(sum(metrics["migration_jobs_completed"]))
        stage_results[name] = {
            "durationSeconds": seconds,
            "apiP50Ms": median(api_latency) if api_latency else None,
            "apiP95Ms": p95(api_latency),
            "apiRequests": requests,
            "apiRequestsPerSecond": requests / seconds if seconds else None,
            "apiErrorRate": sum(api_errors) / len(api_errors) if api_errors else None,
            "queueWaitP95Ms": p95(metrics["queue_wait_ms"]),
            "jobDurationP95Ms": p95(metrics["job_duration_ms"]),
            "jobsAccepted": int(sum(metrics["migration_jobs_accepted"])),
            "jobsTerminal": completed,
            "jobsTerminalPerSecond": completed / seconds if seconds else None,
            "manifestExhausted": int(sum(metrics["migration_case_manifest_exhausted"])),
        }
    return stage_results, statuses


def run(app, folder):
    fixture = json.loads((folder / "fixture.json").read_text(encoding="utf-8"))
    manifest = json.loads((folder / "cases.json").read_text(encoding="utf-8"))
    if not manifest or len(manifest) != fixture["caseCount"]:
        raise RuntimeError("Jumlah migration dalam manifest tidak cocok dengan fixture.")
    if (folder / "run.json").exists():
        raise RuntimeError("Fixture sudah dipakai. Jalankan prepare untuk membuat fixture baru.")

    cookie_name = os.environ.get("SESSION_COOKIE_NAME", "dbmp_session")
    cookie = next(cookie.value for cookie in app.cookies if cookie.name == cookie_name)
    env = {
        "DBMP_API_BASE_URL": "http://backend:8080",
        "DBMP_ORIGIN": app.origin,
        "DBMP_CASES_FILE": "/run-data/cases.json",
        "DBMP_SESSION_COOKIE_NAME": cookie_name,
        "DBMP_SESSION_COOKIE": cookie,
        "DBMP_CSRF_TOKEN": app.csrf,
        "DBMP_K6_SMOKE": "1" if fixture["smoke"] else "0",
    }
    metadata = {
        **fixture,
        "k6Image": IMAGE,
        "sourceCommit": docker("git", "rev-parse", "HEAD", capture_output=True).stdout.strip(),
        "workingTreeDirty": bool(docker("git", "status", "--porcelain", capture_output=True).stdout),
        "scriptSha256": hashlib.sha256(SCRIPT.read_bytes()).hexdigest(),
        "baselineVUs": BASELINE_VUS,
        "startedAtUtc": utc_now(),
    }
    write_json(folder / "run.json", metadata)
    args = ["docker", "run", "--rm", "--network", "wbd-k08-m2test_default",
            "--user", f"{os.getuid()}:{os.getgid()}",
            "-v", f"{SCRIPT.parent}:/k6:ro", "-v", f"{folder}:/run-data"]
    for key in env:
        args.extend(("-e", key))
    result = subprocess.run([
        *args, IMAGE, "run", "--summary-export=/run-data/summary.json",
        "--out", "json=/run-data/samples.json.gz", "/k6/migration-queue.js",
    ], cwd=ROOT, env={**os.environ, **env})

    metadata.update(finishedAtUtc=utc_now(), k6ExitCode=result.returncode)
    samples = folder / "samples.json.gz"
    if samples.exists():
        stages, statuses = summarize(samples, fixture)
        accepted = sum(stage["jobsAccepted"] for stage in stages.values())
        terminal = sum(statuses.values())
        metadata.update(
            stages=stages, jobsAccepted=accepted, jobsTerminal=terminal,
            terminalStatuses=statuses, jobsUnobserved=max(0, accepted - terminal),
            manifestExhausted=sum(stage["manifestExhausted"] for stage in stages.values()),
        )
    write_json(folder / "run.json", metadata)
    print(f"Hasil: {folder.relative_to(ROOT)}/run.json")
    if result.returncode:
        return result.returncode
    if metadata.get("jobsAccepted", 0) == 0 or metadata.get("jobsUnobserved") != 0:
        return 1
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    setup = commands.add_parser("prepare", help="Create fresh databases and approved migrations")
    setup.add_argument("--smoke", action="store_true", help="Create four cases for a quick smoke run")
    execute = commands.add_parser("run", help="Run k6 against a prepared local fixture")
    execute.add_argument("folder", type=Path, help="Fixture folder printed by prepare")
    args = parser.parse_args()
    app = App()
    if args.command == "prepare":
        stamp = datetime.now(timezone.utc).strftime("%Y%m%d_%H%M%S")
        folder = ROOT / "tmp/k6" / f"k6_{stamp}_{secrets.token_hex(3)}"
        folder.mkdir(parents=True, exist_ok=False)
        print(f"Menyiapkan fixture {folder.relative_to(ROOT)}", flush=True)
        prepare(app, folder, args.smoke)
        return 0
    folder = args.folder.resolve()
    if folder.parent != ROOT / "tmp/k6":
        parser.error("Gunakan folder fixture di tmp/k6 yang dibuat oleh prepare.")
    return run(app, folder)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (RuntimeError, KeyError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"k6: {error}") from None
