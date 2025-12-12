# StatsIQ Aggregate Worker

Worker Go qui consomme les jobs depuis une queue Redis pour calculer les statistiques agrégées à partir des données canoniques Valorant.

## Vue d'ensemble

Le worker s'inscrit dans le pipeline de traitement des données StatsIQ :

```
Riot API → raw_matches → Canonical Worker → Tables Canoniques → Aggregate Worker → Tables Agrégées
```

### Rôle métier

L'Aggregate Worker transforme les données canoniques (événements bruts par round) en statistiques exploitables pour le dashboard :

- **Statistiques par joueur par round** : Combat Score, kills, deaths, damage, trades, clutches
- **Statistiques par joueur par match** : ACS, K/D, KAST, ADR, multi-kills, clutches par type (1v1 à 1v5)
- **Statistiques par équipe par match** : Win rate, rounds gagnés, multi-kills d'équipe
- **Statistiques par équipe par côté** : Performance Attack vs Defense

### Calculs effectués

| Métrique | Description | Logique |
|----------|-------------|---------|
| **Combat Score (CS)** | Score de combat par round | Récupéré depuis `round_player_state.score` |
| **ACS** | Average Combat Score | Moyenne des CS sur tous les rounds joués |
| **K/D** | Kill/Death ratio | `kills / deaths` |
| **KAST** | Kill/Assist/Survive/Trade % | % de rounds avec au moins un kill, assist, survie ou trade |
| **ADR** | Average Damage per Round | `total_damage / rounds_played` |
| **Multi-kills** | Séries de kills rapides | Kills consécutifs avec < 5 secondes entre chaque |
| **Trades** | Kills/deaths tradés | Kill/death dans les 3 secondes suivant la mort d'un coéquipier |
| **Entry kills** | Premier kill du round | Détection du premier kill de chaque round |
| **Clutches** | Situations 1vX | Détection quand un joueur reste seul contre plusieurs adversaires |
| **Suicides** | Morts auto-infligées | `killer_id == victim_id` et `weapon != "Spike"` |
| **Deaths by Spike** | Morts par explosion | `killer_id == victim_id` et `weapon == "Spike"` |
| **Team kills** | Kills sur coéquipiers | `killer_team == victim_team` |

## Architecture technique

```
cmd/worker/main.go          # Point d'entrée
internal/
├── config/config.go        # Configuration depuis variables d'environnement
├── logging/logger.go       # Logger structuré (zerolog)
├── queue/redis.go          # Consumer Redis (BRPOP)
├── db/
│   ├── pg.go               # Pool de connexions PostgreSQL
│   ├── canonical_reader.go # Lecture des tables canoniques
│   └── aggregate_writer.go # Écriture des tables agrégées (COPY protocol)
├── processor/
│   └── aggregate_job.go    # Orchestrateur du traitement d'un job
└── aggregate/
    ├── model.go            # Structs et constantes
    ├── builder.go          # Orchestrateur des calculs
    ├── entries.go          # Détection des entry kills
    ├── trades.go           # Détection des trades (3s window)
    ├── clutches.go         # Détection des clutches (1vX)
    ├── multikills.go       # Détection des multi-kills (5s window)
    ├── round_player_stats.go   # Stats par joueur par round
    ├── match_player_stats.go   # Stats par joueur par match
    ├── team_stats.go           # Stats par équipe par match
    └── team_side_stats.go      # Stats par équipe par côté
```

### Flux de traitement

1. **Consommation** : Le worker écoute la queue Redis `aggregate_matches` via `BRPOP`
2. **Lecture** : Récupération des données canoniques (matches, rounds, events, players)
3. **Calcul** : Exécution séquentielle des algorithmes de détection
4. **Écriture** : Insertion transactionnelle avec advisory lock et purge préalable

### Tables sources (canoniques)

| Table | Données |
|-------|---------|
| `matches` | Métadonnées du match (scores, type) |
| `rounds` | Informations par round (winner, spike events) |
| `round_events` | Événements kill/damage avec timestamps |
| `round_player_state` | État du joueur par round (score) |
| `round_player_loadouts` | Économie (crédits dépensés/restants) |
| `match_players` | Association joueur-équipe-agent |
| `players` | Identité des joueurs |

### Tables cibles (agrégées)

| Table | Contenu |
|-------|---------|
| `clutches` | Situations de clutch détectées |
| `round_player_stats_agregate` | Stats par joueur par round |
| `match_player_stats_agregate` | Stats par joueur par match |
| `team_match_stats_agregate` | Stats par équipe par match |
| `team_match_side_stats_agregate` | Stats par équipe par côté |

## Configuration

| Variable | Requis | Default | Description |
|----------|--------|---------|-------------|
| `DB_URL` | Oui | - | URL de connexion PostgreSQL |
| `REDIS_URL` | Oui | - | URL de connexion Redis |
| `REDIS_QUEUE` | Non | `aggregate_matches` | Nom de la queue Redis |
| `WORKER_COUNT` | Non | `4` | Nombre de workers concurrents |
| `JOB_BUFFER_SIZE` | Non | `100` | Taille du buffer de jobs |

### Exemple

```bash
export DB_URL="postgres://statsiq:statsiq@localhost:5432/statsiq?sslmode=disable"
export REDIS_URL="redis://localhost:6379"
export REDIS_QUEUE="aggregate_matches"
```

## Format du job Redis

```json
{
  "match_id": "706aed68-d665-4479-ab2b-52ecab0ea502"
}
```

Le `match_id` correspond à l'UUID de la table `matches` (pas le `match_id` Riot).

## Idempotence

Le worker est idempotent grâce à :

1. **Advisory lock** sur le match_id pour éviter les traitements concurrents
2. **Purge préalable** des données agrégées existantes avant insertion
3. **Transaction atomique** : toutes les insertions dans une seule transaction

## Traitement concurrent

Le worker supporte le traitement concurrent via un pool de workers.

### Architecture

```
┌─────────────────┐
│   Redis Queue   │
│ (BRPOP loop)    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Job Channel   │
│ (buffered: 100) │
└────────┬────────┘
         │
    ┌────┴────┬────────┬────────┐
    ▼         ▼        ▼        ▼
┌───────┐ ┌───────┐ ┌───────┐ ┌───────┐
│Worker0│ │Worker1│ │Worker2│ │Worker3│
└───────┘ └───────┘ └───────┘ └───────┘
```

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKER_COUNT` | `4` | Nombre de workers parallèles |
| `JOB_BUFFER_SIZE` | `100` | Taille du buffer du channel |

### Comportement

- Si `WORKER_COUNT > 1` : Utilise `ConsumeConcurrent` avec un pool de workers
- Si `WORKER_COUNT = 1` : Utilise `Consume` single-threaded (comportement original)

### Performance

Traitement de 100 matches :

| Configuration | Durée |
|---------------|-------|
| 1 worker | ~2-3s |
| 4 workers | ~1s |

### Logs

```json
{"level":"info","message":"starting concurrent consumption with 4 workers"}
{"level":"info","message":"started 4 concurrent workers for queue aggregate_matches"}
{"level":"info","message":"aggregate job completed for match xxx in 10.5ms"}
```

### Gestion des erreurs

Chaque worker gère ses propres retries :
- Jobs échoués → `aggregate_matches:retry`
- Après 3 tentatives → `aggregate_matches:dlq` (dead letter queue)
- Compteur de retry basé sur le hash SHA256 du payload

## Algorithmes de détection

### Multi-kills (5 secondes)

Un multi-kill est une série de kills où chaque kill consécutif est effectué dans les 5 secondes suivant le précédent.

```
Exemple : Joueur A tue à t=15s, t=16s, t=30s, t=31s
- Série 1 : t=15s, t=16s (1s d'écart) → Double kill
- Série 2 : t=30s, t=31s (1s d'écart) → Double kill
- Total : 2 multi-kills, 2 doubles
```

### Trades (3 secondes)

Un trade kill se produit quand un joueur tue l'adversaire qui vient de tuer son coéquipier dans les 3 secondes.

```
Exemple : Adversaire A tue Coéquipier B à t=10s
          Joueur C tue Adversaire A à t=12s
→ Joueur C a un trade kill, Coéquipier B a une traded death
```

### Clutches

Un clutch est détecté quand un joueur se retrouve seul contre plusieurs adversaires après un délai de confirmation de 3 secondes.

```
Exemple : Round avec 5v5
- t=10s : 4 coéquipiers meurent, joueur reste 1v3
- t=13s : Situation confirmée comme clutch 1v3
- t=20s : Joueur gagne le round
→ Clutch 1v3 gagné
```

### Détermination des côtés (Attack/Defense)

- **Rounds 0-11** : RED = Attack, BLUE = Defense
- **Rounds 12-23** : RED = Defense, BLUE = Attack
- **Overtime (24+)** : Alternance tous les 2 rounds

## Test du worker

### Prérequis

Docker Compose avec les services `statsiq_db`, `statsiq_redis`, `statsiq_canonical_worker` et `statsiq_aggregate_worker`.

### Étape 1 : Démarrer l'infrastructure

```powershell
cd C:\Users\Administrator\Documents\statsiq_app
docker compose -f docker-compose.dev.yml up -d statsiq_db statsiq_redis
```

Attendre que PostgreSQL soit prêt (~5 secondes).

### Étape 2 : Réinitialiser la base de données

```powershell
# Arrêter et supprimer le volume de la base
docker compose -f docker-compose.dev.yml down statsiq_db -v

# Redémarrer (applique init_v4.sql)
docker compose -f docker-compose.dev.yml up -d statsiq_db statsiq_redis
```

Attendre ~10 secondes pour l'initialisation.

### Étape 3 : Charger les assets

```powershell
docker compose -f docker-compose.dev.yml up -d statsiq_assets_service

# Attendre le chargement (~30 secondes)
Start-Sleep -Seconds 30

# Vérifier
docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -c "SELECT COUNT(*) FROM asset_agents;"
```

### Étape 4 : Insérer un match de test

```powershell
# Créer le fichier SQL d'insertion
$matchJson = Get-Content "C:\Users\Administrator\Documents\statsiq_app\mock_riot_api\test_match\fake_match.json" -Raw
$escapedJson = $matchJson -replace "'", "''"

$sql = @"
INSERT INTO raw_matches (id, match_id, raw_json, processed, created_at)
VALUES (
  '11111111-1111-1111-1111-111111111111',
  '97ccb0b5-394c-4563-ab0b-8977b195dd66',
  '${escapedJson}'::jsonb,
  false,
  now()
)
ON CONFLICT (id) DO UPDATE SET processed = false, raw_json = EXCLUDED.raw_json;
"@

$sql | Set-Content "C:\Users\Administrator\Documents\statsiq_app\insert_match.sql"

# Exécuter l'insertion
docker cp "C:\Users\Administrator\Documents\statsiq_app\insert_match.sql" statsiq_app-statsiq_db-1:/tmp/insert_match.sql
docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -f /tmp/insert_match.sql
```

### Étape 5 : Traitement canonique

```powershell
# Démarrer le canonical worker
docker compose -f docker-compose.dev.yml up -d --build statsiq_canonical_worker

# Envoyer le job au canonical worker
docker exec statsiq_app-statsiq_redis-1 redis-cli LPUSH canonical_matches '{"ingest_id":"11111111-1111-1111-1111-111111111111"}'

# Attendre le traitement
Start-Sleep -Seconds 5

# Vérifier les logs
docker compose -f docker-compose.dev.yml logs statsiq_canonical_worker --tail 10

# Vérifier la création du match canonique
docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -c "SELECT id, match_id FROM matches LIMIT 1;"
```

### Étape 6 : Traitement agrégé

```powershell
# Récupérer l'ID du match canonique
$matchId = docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -t -c "SELECT id FROM matches LIMIT 1;"
$matchId = $matchId.Trim()

# Démarrer l'aggregate worker
docker compose -f docker-compose.dev.yml up -d --build statsiq_aggregate_worker

# Envoyer le job (le canonical worker l'a normalement déjà fait)
docker exec statsiq_app-statsiq_redis-1 redis-cli LPUSH aggregate_matches "{`"match_id`":`"$matchId`"}"

# Attendre le traitement
Start-Sleep -Seconds 5

# Vérifier les logs
docker compose -f docker-compose.dev.yml logs statsiq_aggregate_worker --tail 15
```

### Étape 7 : Vérifier les résultats

```powershell
# Stats par joueur par match
docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -c "
SELECT player_id, acs, kd, kills, deaths, multi_kills, clutches_won
FROM match_player_stats_agregate
ORDER BY acs DESC;"

# Stats par équipe
docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -c "
SELECT team_id, rounds_won, rounds_lost, total_kills, multikill, avg_acs
FROM team_match_stats_agregate;"

# Stats par côté
docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -c "
SELECT team_id, team_side, rounds_won, rounds_lost, total_kills, multikill
FROM team_match_side_stats_agregate
ORDER BY team_id, team_side;"

# Clutches
docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -c "
SELECT c.side, c.type, c.won, c.situation, r.round_number
FROM clutches c
JOIN rounds r ON c.round_id = r.id
WHERE c.is_clutcher = true
ORDER BY r.round_number
LIMIT 10;"
```

### Réexécuter le traitement agrégé

Pour retraiter un match (utile après modification du code) :

```powershell
# Récupérer les IDs
$matchId = docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -t -c "SELECT id FROM matches LIMIT 1;"
$matchId = $matchId.Trim()

# Supprimer les données agrégées existantes
docker exec statsiq_app-statsiq_db-1 psql -U statsiq -d statsiq -c "
DELETE FROM match_player_stats_agregate WHERE match_id = '$matchId';
DELETE FROM round_player_stats_agregate WHERE round_id IN (SELECT id FROM rounds WHERE match_id = '$matchId');
DELETE FROM team_match_stats_agregate WHERE match_id = '$matchId';
DELETE FROM team_match_side_stats_agregate WHERE match_id = '$matchId';
DELETE FROM clutches WHERE round_id IN (SELECT id FROM rounds WHERE match_id = '$matchId');
"

# Rebuild et redémarrer le worker
docker compose -f docker-compose.dev.yml up -d --build statsiq_aggregate_worker

# Envoyer le job
docker exec statsiq_app-statsiq_redis-1 redis-cli LPUSH aggregate_matches "{`"match_id`":`"$matchId`"}"

# Vérifier
Start-Sleep -Seconds 5
docker compose -f docker-compose.dev.yml logs statsiq_aggregate_worker --tail 10
```

## Développement

### Build local

```bash
cd statsiq_aggregate_worker
go build -o aggregate_worker ./cmd/worker
```

### Exécution locale

```bash
export DB_URL="postgres://statsiq:statsiq@localhost:5432/statsiq?sslmode=disable"
export REDIS_URL="redis://localhost:6379"
./aggregate_worker
```

### Hot reload (Docker)

Le conteneur utilise Air pour le hot reload. Les modifications dans `internal/` déclenchent un rebuild automatique.

### Logs

Les logs sont structurés en JSON :

```json
{"level":"info","time":"2025-12-09T22:48:02Z","message":"processing aggregate job for match 706aed68-d665-4479-ab2b-52ecab0ea502"}
{"level":"info","time":"2025-12-09T22:48:02Z","message":"loaded canonical data: 26 rounds, 10 players, 534 events"}
{"level":"info","time":"2025-12-09T22:48:02Z","message":"computed aggregates: 74 clutches, 260 round_player_stats, 10 match_player_stats, 2 team_stats, 4 side_stats"}
{"level":"info","time":"2025-12-09T22:48:02Z","message":"aggregate job completed for match 706aed68-d665-4479-ab2b-52ecab0ea502 in 37.935577ms"}
```
