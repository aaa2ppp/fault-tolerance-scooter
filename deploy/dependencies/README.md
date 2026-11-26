# Инфраструктура ScooterFlow

Kustomize описывает namespace scooterflow, отдельные PostgreSQL rental/pricing/
profile/scooter, Kafka KRaft, AKHQ, Valkey Cluster и прикладной config-etcd. У каждого PostgreSQL
свой PVC и Secret. Все credentials синтетические, для локального курса.

scooterctl рендерит эти manifests, затем создаёт immutable Job с hash канонических
SQL-миграций и seed из services/*/tools. Последовательно выполняет Goose в каждой
БД, создаёт rental.events.v1 и user-discount-invalidate.v1 и применяет seed. Изменение SQL меняет имя Job.
Никаких SQL-копий схемы в deploy нет. Только Rental и Pricing получают Kafka env.
Gateway не получает credentials PostgreSQL.

Valkey хранит данные шести узлов в отдельных PVC. При запуске valkey-0 собирает три шарда с одной репликой на каждый. На существующих томах узлы восстанавливают связи. `scooterctl up` ждёт готовности всех шести перед запуском Profile.
