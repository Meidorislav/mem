#!/usr/bin/env bash
# Calibrate --min-score for an embedding model.
#
# Seeds a throwaway database (MEM_HOME in a temp dir, your ~/.mem is not
# touched) with realistic memories, then asks questions whose right answer
# is known, phrased with different words than the memory, plus questions
# that have no answer at all. Compare the scores: the threshold belongs
# between the worst right answer and the best answer to an unrelated
# question.
#
# Usage:  scripts/calibrate.sh              # default model (bge-m3)
#         MODEL=nomic-embed-text scripts/calibrate.sh
# Needs Ollama running and the model pulled; run `make build` first.

set -euo pipefail

MEM=${MEM:-./mem}
MEM_HOME=$(mktemp -d "${TMPDIR:-/tmp}/mem-calibrate.XXXXXX")
export MEM_HOME
trap 'rm -rf "$MEM_HOME"' EXIT

save() { "$MEM" save "$@" >/dev/null; }

echo "Seeding memories into $MEM_HOME ..."

save "nginx 502 Bad Gateway: upstream не отвечал" \
  -c "sudo tail -n 100 /var/log/nginx/error.log" \
  -c "sudo systemctl restart api" \
  -c "sudo nginx -t && sudo systemctl reload nginx" -t nginx
save "Postgres: включить логическую репликацию" \
  -c "sudo sed -i 's/^#\?wal_level.*/wal_level = logical/' /etc/postgresql/15/main/postgresql.conf" \
  -c "sudo systemctl restart postgresql" -t postgres
save "удалить все пустые папки" -c "find . -type d -empty -delete"
save "list files by size" -c "ls -lhS"
save "размер директорий в текущей папке" -c "du -sh * | sort -h" -t disk
save "найти файлы больше 500 МБ" -c "find / -type f -size +500M -exec ls -lh {} + 2>/dev/null" -t disk
save "Docker: почистить неиспользуемые образы и кэш" -c "docker system prune -af --volumes" -t docker
save "Docker DNS не резолвит внутри контейнера" \
  -c "docker run --rm alpine cat /etc/resolv.conf" \
  -c "sudo systemctl restart docker" -t docker
save "git: отменить последний коммит, оставив изменения" -c "git reset --soft HEAD~1" -t git
save "git: найти коммит, где сломался тест" \
  -c "git bisect start" -c "git bisect bad" -c "git bisect good v1.2.0" \
  -c "git bisect run go test ./..." -t git
save "kill process listening on port 8080" -c "lsof -ti :8080 | xargs kill -9" -t network
save "SSH туннель к базе на проде" -c "ssh -N -L 5433:localhost:5432 deploy@prod.example.com" -t ssh -t postgres
save "сгенерировать ssh ключ ed25519" -c "ssh-keygen -t ed25519 -C me@laptop" -t ssh
save "Let's Encrypt: продлить сертификат" \
  -c "sudo certbot renew --dry-run" -c "sudo certbot renew" \
  -c "sudo systemctl reload nginx" -t tls -t nginx
save "Kubernetes: логи упавшего пода" \
  -c "kubectl get pods -n api" \
  -c "kubectl logs -n api deploy/api --previous --tail=200" -t k8s
save "k8s: перезапустить деплоймент без даунтайма" \
  -c "kubectl rollout restart deployment/api -n api" \
  -c "kubectl rollout status deployment/api -n api" -t k8s
save "journalctl: логи сервиса за последний час" -c "journalctl -u api --since '1 hour ago' --no-pager" -t systemd
save "Go: найти гонки данных в тестах" -c "go test -race ./..." -t go
save "Go: профилировать CPU" -c "go test -cpuprofile cpu.out -bench ." -c "go tool pprof -http=:8081 cpu.out" -t go
save "macOS: сбросить DNS кэш" -c "sudo dscacheutil -flushcache" -c "sudo killall -HUP mDNSResponder" -t macos -t dns
save "curl: код ответа и время запроса" -c "curl -sS -o /dev/null -w '%{http_code} %{time_total}s\n' https://example.com" -t http
save "tar: распаковать архив в папку" -c "tar -xzf archive.tar.gz -C /opt/app"
save "Redis: найти ключи по шаблону без KEYS" -c "redis-cli --scan --pattern 'session:*' | head" -t redis
save "cron: запускать бэкап каждую ночь в 3:00" -c "0 3 * * * /usr/local/bin/backup.sh >> /var/log/backup.log 2>&1" -t cron

if [ -n "${MODEL:-}" ]; then
  "$MEM" reindex --model "$MODEL" | tail -n 1
fi

# ask prints "N. title  [tags]  (score)"; keep only those lines.
ask() {
  local kind=$1 query=$2 expect=$3
  echo
  echo "=== [$kind] $query   -> $expect"
  "$MEM" ask "$query" -n 3 --min-score 0 2>/dev/null | grep -E '^[0-9]+\. ' || echo "(nothing)"
}

# Right answer known; words differ from the memory on purpose.
ask rel "как я чинил 502 на проде" "nginx 502"
ask rel "how to free disk space taken by docker" "Docker: почистить"
ask rel "откатить коммит но не потерять правки" "git reset --soft"
ask rel "what is using port 8080" "kill process listening on port 8080"
ask rel "подключиться к продовой базе с ноутбука" "SSH туннель"
ask rel "https сертификат скоро истекает" "Let's Encrypt"
ask rel "pod keeps crashing, why" "Kubernetes: логи"
ask rel "перезапустить приложение в кубере" "k8s: перезапустить"
ask rel "почему тормозит программа на go" "Go: профилировать CPU"
ask rel "сайт не открывается после смены dns на маке" "macOS: сбросить DNS"
ask rel "какие файлы занимают больше всего места" "найти файлы больше 500 МБ / list files by size"
ask rel "set up postgres replication" "Postgres: логическая репликация"
ask rel "в каком коммите появился баг" "git bisect"
ask rel "remove empty directories" "удалить все пустые папки"
ask rel "посмотреть логи systemd сервиса" "journalctl"
ask rel "запланировать задачу на ночь" "cron"

# No right answer exists.
ask none "рецепт борща" "-"
ask none "how to center a div in css" "-"
ask none "настроить vpn на роутере" "-"
ask none "python virtualenv activate" "-"
ask none "дни рождения коллег" "-"
