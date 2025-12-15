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
| **Duels** | Stats head-to-head entre joueurs | Kills/deaths/damage entre chaque paire de joueurs adverses |
| **Weapon stats** | Performance par arme | Kills/deaths/damage par arme utilisée |

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
│   ├── aggregate_writer.go # Écriture des tables agrégées (COPY protocol)
│   └── ca_refresher.go     # Rafraîchissement des CAs après chaque match
├── processor/
│   └── aggregate_job.go    # Orchestrateur du traitement d'un job
└── aggregate/
    ├── model.go            # Structs et constantes
    ├── builder.go          # Orchestrateur des calculs
    ├── entries.go          # Détection des entry kills
    ├── trades.go           # Détection des trades (3s window)
    ├── clutches.go         # Détection des clutches (1vX)
    ├── multikills.go       # Détection des multi-kills (5s window)
    ├── duels.go            # Stats head-to-head entre joueurs
    ├── weapon_stats.go     # Stats par arme par joueur
    ├── denormalized_stats.go   # Stats dénormalisées pour les CAs
    ├── round_team_stats.go     # Stats par équipe par round (pour CAs)
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
| `round_team_stats_agregate` | Stats par équipe par round (pour CAs de composition) |
| `match_player_stats_agregate` | Stats par joueur par match |
| `team_match_stats_agregate` | Stats par équipe par match |
| `team_match_side_stats_agregate` | Stats par équipe par côté |
| `match_player_duels_agregate` | Stats head-to-head entre paires de joueurs |
| `match_player_weapon_stats_agregate` | Stats par arme par joueur |

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

1. **Global advisory lock** partagé avec `statsiq_canonical_worker` pour éviter les deadlocks cross-workers
2. **Advisory lock** sur le match_id pour éviter les traitements concurrents du même match
3. **Purge préalable** des données agrégées existantes avant insertion
4. **Transaction atomique** : toutes les insertions dans une seule transaction

## Prévention des Deadlocks

### Problème

Le canonical_worker et l'aggregate_worker opèrent sur des tables liées (FK entre `rounds` et `round_player_stats_agregate`). Quand les deux workers écrivent en parallèle, ils peuvent acquérir des row locks dans des ordres différents :

```
Canonical Worker (Match A): INSERT rounds → lock row R1
Aggregate Worker (Match B): INSERT round_player_stats_agregate → needs FK on R1 → BLOCKED
                           ... pendant que Canonical veut créer des FK references
→ DEADLOCK
```

### Solution : Advisory Lock Partagé

Les deux workers utilisent le **même advisory lock global** avant toute écriture :

```go
// aggregate_writer.go
const globalWriteLockKey int64 = 0x7374617469717721 // "statsiq_write"

func (w *AggregateWriter) WriteAll(ctx context.Context, data *aggregate.AggregateSet) error {
    tx, _ := w.pool.Begin(ctx)

    // 1. Acquire shared global lock (same key as canonical_worker)
    tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, globalWriteLockKey)

    // 2. Acquire match-specific lock
    tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, matchLockKey)

    // 3. Purge existing data
    // 4. Insert new aggregates
    // 5. Commit → releases all locks
}
```

### Pourquoi Cette Architecture ?

| Composant | Rôle |
|-----------|------|
| `globalWriteLockKey` | Sérialise TOUTES les écritures des deux workers |
| Match-specific lock | Empêche le retraitement concurrent du même match |
| Purge préalable | Permet le retraitement idempotent |

### Impact Performance

Le lock global sérialise les écritures mais :
- Les phases de lecture (canonical data) restent parallèles
- Les calculs d'agrégation restent parallèles
- Seule l'écriture finale est sérialisée (~10-50ms par match)

Résultat : **0 deadlock** sur 93 matchs traités avec `WORKER_COUNT=4` pour les deux workers.

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

Traitement de 93 matchs (7 incomplets exclus) :

| Configuration | Durée | Notes |
|---------------|-------|-------|
| 1 worker | ~2-3s | Séquentiel |
| 4 workers | ~1s | Parallèle avec lock global |
| Pipeline complet (canonical + aggregate) | ~90s | Avec les deux workers à WORKER_COUNT=4 |

Note : Le lock global sérialise les écritures mais les phases de lecture et calcul restent parallèles.

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

## Continuous Aggregates (CAs)

### Rafraîchissement Immédiat par le Worker

**Le worker rafraîchit automatiquement toutes les CAs après chaque match traité.**

Après l'écriture des données agrégées, le worker appelle `CARefresher.RefreshForMatchDate()` qui :
1. Calcule une fenêtre de rafraîchissement : `[matchDate - 1 jour, matchDate + 1 jour]`
2. Rafraîchit les **23 CAs** dans cette fenêtre
3. Log le résultat (succès ou échec partiel)

```go
// aggregate_job.go - appelé après chaque match
if p.caRefresher != nil {
    if err := p.caRefresher.RefreshForMatchDate(p.ctx, data.MatchDate); err != nil {
        logger.Warnf("CA refresh failed for match %s: %v", matchID, err)
    }
}
```

**Résultat** : Les données sont visibles dans le dashboard **immédiatement** après le traitement d'un match, sans attendre le rafraîchissement automatique de TimescaleDB.

### Architecture TimescaleDB

Les données agrégées écrites par ce worker alimentent les **Continuous Aggregates** de TimescaleDB, qui pré-calculent les statistiques pour le dashboard :

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Aggregate Worker                                       │
│                                                                               │
│  Canonical Data ──▶ Calculs ──▶ Hypertables (tables agrégées)               │
│                                      │                                        │
│                    ┌─────────────────┴─────────────────┐                      │
│                    ▼                                   ▼                      │
│       ┌─────────────────────────┐       ┌─────────────────────────┐          │
│       │  CARefresher (immédiat) │       │ TimescaleDB Auto Refresh│          │
│       │  Après chaque match     │       │  (toutes les 5-10 min)  │          │
│       └───────────┬─────────────┘       └───────────┬─────────────┘          │
│                   │                                 │                         │
│                   └─────────────┬───────────────────┘                         │
│                                 ▼                                             │
│                   ┌─────────────────────────┐                                 │
│                   │  Continuous Aggregates  │                                 │
│                   │  (23 vues matérialisées)│                                 │
│                   └───────────┬─────────────┘                                 │
│                               ▼                                               │
│                   ┌─────────────────────────┐                                 │
│                   │  statsiq_api Dashboard  │                                 │
│                   │  (queries rapides ~50ms)│                                 │
│                   └─────────────────────────┘                                 │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Double Stratégie de Rafraîchissement

| Mécanisme | Déclencheur | Fenêtre | Latence |
|-----------|-------------|---------|---------|
| **CARefresher** (worker) | Après chaque match | ±1 jour autour de `matchDate` | Immédiat (~2-5s) |
| **TimescaleDB Policy** | Automatique | 7 derniers jours | 5-10 minutes |

**Pourquoi les deux ?**
- **CARefresher** : Garantit la visibilité immédiate après traitement
- **TimescaleDB Policy** : Filet de sécurité si le refresh du worker échoue, et recalcul périodique pour corriger d'éventuelles inconsistances

### Tables Hypertables → CAs

| Hypertable (écrite par worker) | Continuous Aggregates alimentées |
|--------------------------------|----------------------------------|
| `match_player_stats_agregate` | `ca_player_daily_stats`, `ca_team_player_daily_stats`, `ca_player_map_stats`, `ca_player_agent_stats` |
| `round_player_stats_agregate` | `ca_player_side_daily_stats`, `ca_player_economy_daily_stats`, `ca_player_situation_stats` |
| `team_match_stats_agregate` | `ca_team_daily_stats`, `ca_composition_daily_stats` |
| `team_match_side_stats_agregate` | `ca_team_outcome_daily_stats` |
| `match_player_weapon_stats_agregate` | `ca_player_weapon_daily_stats` |
| `player_clutch_stats_agregate` | `ca_player_clutch_stats` |
| `round_team_stats_agregate` | `ca_composition_economy_stats`, `ca_composition_situation_stats` |

### Politique de Rafraîchissement

Les CAs sont automatiquement rafraîchies par TimescaleDB selon cette configuration :

```sql
SELECT add_continuous_aggregate_policy('ca_team_daily_stats',
    start_offset => INTERVAL '7 days',   -- Recalcule les 7 derniers jours
    end_offset => INTERVAL '1 minute',   -- Jusqu'à 1 minute avant maintenant
    schedule_interval => INTERVAL '5 minutes'  -- Toutes les 5 minutes
);
```

| Paramètre | Valeur | Description |
|-----------|--------|-------------|
| `start_offset` | 7 jours | Fenêtre de recalcul (données récentes pouvant être mises à jour) |
| `end_offset` | 1 minute | Marge pour éviter les données "en vol" |
| `schedule_interval` | 5-10 min | Fréquence de rafraîchissement automatique |

### Real-Time Aggregates

Les CAs sont configurées avec `materialized_only = false` :

```sql
ALTER MATERIALIZED VIEW ca_team_daily_stats SET (timescaledb.materialized_only = false);
```

Cela signifie :
- **Données matérialisées** : Buckets complètement rafraîchis (< `end_offset`)
- **Données temps réel** : Le bucket courant est calculé à la volée depuis les hypertables

**Avantage** : Les nouvelles données sont visibles immédiatement dans le dashboard, même avant le prochain rafraîchissement.

### Liste des CAs Dashboard (Team)

| CA | Données | Intervalle |
|----|---------|------------|
| `ca_team_daily_stats` | Win rate, K/D, ADR, FK%, Clutch WR | 5 min |
| `ca_team_player_daily_stats` | Scoreboard (General, Advanced, Accuracy, Clutch) | 5 min |
| `ca_team_map_daily_stats` | Performance par map | 10 min |
| `ca_team_agent_daily_stats` | Usage et WR par agent | 10 min |
| `ca_team_outcome_daily_stats` | Stats Win/Lose | 10 min |
| `ca_team_player_duels_daily_stats` | Duels (Accuracy tab) | 10 min |

### Rafraîchissement Manuel

Pour forcer le rafraîchissement d'une CA (utile après un batch d'imports) :

```sql
-- Rafraîchir les 7 derniers jours de ca_team_daily_stats
CALL refresh_continuous_aggregate('ca_team_daily_stats',
    now() - INTERVAL '7 days',
    now()
);

-- Rafraîchir toutes les CAs team (script)
DO $$
DECLARE
    ca_name TEXT;
BEGIN
    FOR ca_name IN SELECT 'ca_team_daily_stats' UNION ALL
                   SELECT 'ca_team_player_daily_stats' UNION ALL
                   SELECT 'ca_team_map_daily_stats' UNION ALL
                   SELECT 'ca_team_agent_daily_stats' UNION ALL
                   SELECT 'ca_team_outcome_daily_stats' UNION ALL
                   SELECT 'ca_team_player_duels_daily_stats'
    LOOP
        EXECUTE format('CALL refresh_continuous_aggregate(%L, now() - INTERVAL ''7 days'', now())', ca_name);
    END LOOP;
END $$;
```

### Vérification du Statut

```sql
-- Voir les jobs de rafraîchissement actifs
SELECT * FROM timescaledb_information.jobs
WHERE proc_name = 'policy_refresh_continuous_aggregate';

-- Voir le dernier rafraîchissement de chaque CA
SELECT view_name, last_run_started_at, last_run_status
FROM timescaledb_information.job_stats js
JOIN timescaledb_information.jobs j ON js.job_id = j.job_id
WHERE j.proc_name = 'policy_refresh_continuous_aggregate';

-- Vérifier les données matérialisées vs temps réel
SELECT * FROM timescaledb_information.continuous_aggregates;
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
