# Database Backup Container

Image Docker basée sur Debian Slim pour automatiser les backups de bases de données PostgreSQL et MariaDB/MySQL via cron.

## Fonctionnalités

- Support de PostgreSQL et MariaDB/MySQL
- Configuration flexible via fichier de configuration
- Planification des backups avec cron
- Compression automatique des dumps (gzip)
- Horodatage des fichiers de backup
- Rotation automatique des anciens backups
- Support de multiples bases de données simultanément
- Logs centralisés
- Build automatique via GitHub Actions
- Images multi-architecture (amd64, arm64)

## Installation rapide

### Utiliser l'image pré-buildée depuis GitHub Container Registry

Si ce projet est hébergé sur GitHub, vous pouvez utiliser l'image directement sans avoir à la construire :

```bash
docker pull ghcr.io/greite/database-backup:latest
```

Exemple de docker-compose.yml utilisant l'image pré-buildée :

```yaml
version: '3.8'

services:
  db-backup:
    image: ghcr.io/greite/database-backup:latest
    container_name: db-backup
    restart: unless-stopped
    volumes:
      - ./backups:/backups
      - ./backups.conf:/config/backups.conf:ro
    networks:
      - db-network
```

### Tags disponibles

- `latest` - Dernière version stable de la branche principale
- `main-<sha>` - Version spécifique basée sur un commit
- `v1.0.0` - Version taggée (si vous créez des releases)

## Structure du projet

```
.
├── Dockerfile
├── docker-compose.yml          # Exemple avec bases de données de test
├── backups.conf                # Configuration des backups
├── backups.conf.example        # Exemple de configuration
├── GITHUB_SETUP.md            # Guide de configuration GitHub Actions
├── .github/
│   └── workflows/
│       └── docker-build.yml   # Workflow de build automatique
├── scripts/
│   ├── backup.sh              # Script de backup principal
│   └── entrypoint.sh          # Script d'initialisation
└── backups/                    # Répertoire des backups (créé automatiquement)
    ├── postgres/
    │   └── myapp_db/
    └── mariadb/
        └── wordpress/
```

## Configuration

### Format du fichier backups.conf

Le fichier `backups.conf` définit les backups à effectuer. Chaque ligne représente un backup avec le format suivant :

```
CRON_SCHEDULE|TYPE|HOST|PORT|DATABASE|USER|PASSWORD|RETENTION_DAYS
```

**Champs :**

- `CRON_SCHEDULE` : Expression cron standard (ex: `0 2 * * *` pour 2h du matin chaque jour)
- `TYPE` : Type de base de données (`postgres` ou `mariadb` ou `mysql`)
- `HOST` : Nom d'hôte ou adresse IP du serveur de base de données
- `PORT` : Port de connexion (optionnel, par défaut 5432 pour postgres, 3306 pour mariadb)
- `DATABASE` : Nom de la base de données à sauvegarder
- `USER` : Utilisateur de connexion à la base de données
- `PASSWORD` : Mot de passe de connexion (les caractères spéciaux sont supportés)
- `RETENTION_DAYS` : Nombre de jours de rétention (optionnel, par défaut 7)

**Note importante sur les mots de passe :**
Les mots de passe avec caractères spéciaux (`!`, `@`, `#`, `$`, `%`, `^`, `&`, `*`, etc.) sont entièrement supportés. Le système échappe automatiquement tous les caractères spéciaux. Vous n'avez pas besoin d'échapper ou de mettre des guillemets autour de votre mot de passe dans le fichier de configuration.

**Exemples :**

```conf
# Backup PostgreSQL tous les jours à 2h, conserver 14 jours
0 2 * * *|postgres|db-server|5432|myapp|backup_user|SecureP@ss|14

# Backup MariaDB tous les jours à 3h, conserver 7 jours
0 3 * * *|mariadb|mysql-server|3306|wordpress|wp_backup|MyPassword|7

# Backup toutes les 6 heures, conserver 3 jours
0 */6 * * *|postgres|localhost|5432|ecommerce|dbuser|pass123|3

# Backup tous les dimanches à minuit, conserver 30 jours
0 0 * * 0|mariadb|db.example.com||analytics|readonly|secret|30

# Exemple avec un mot de passe contenant des caractères spéciaux
0 4 * * *|postgres|pg-prod|5432|webapp|admin|ZxirfRuipZPHPc^#V#HFpCpRyrQ!zG5W|14
```

### Expressions Cron courantes

```
0 2 * * *      # Tous les jours à 2h du matin
0 */6 * * *    # Toutes les 6 heures
0 0 * * 0      # Tous les dimanches à minuit
30 1 * * *     # Tous les jours à 1h30
0 0 1 * *      # Le 1er de chaque mois à minuit
```

## Utilisation

### Option 1 : Avec Docker Compose (recommandé)

1. Créez votre fichier `backups.conf` :

```bash
cp backups.conf.example backups.conf
# Éditez backups.conf avec vos paramètres
```

2. Lancez les services :

```bash
docker-compose up -d
```

3. Vérifiez les logs :

```bash
docker-compose logs -f db-backup
```

### Option 2 : Docker run

1. Construisez l'image :

```bash
docker build -t db-backup .
```

2. Lancez le container :

```bash
docker run -d \
  --name db-backup \
  -v $(pwd)/backups:/backups \
  -v $(pwd)/backups.conf:/config/backups.conf:ro \
  db-backup
```

3. Vérifiez les logs :

```bash
docker logs -f db-backup
```

## Gestion des backups

### Voir les backups créés

```bash
ls -lh backups/postgres/myapp_db/
ls -lh backups/mariadb/wordpress/
```

### Structure des fichiers de backup

Les backups sont organisés par type et base de données :

```
backups/
├── postgres/
│   └── myapp_db/
│       ├── myapp_db_20250131_020000.sql.gz
│       ├── myapp_db_20250130_020000.sql.gz
│       └── ...
└── mariadb/
    └── wordpress/
        ├── wordpress_20250131_030000.sql.gz
        ├── wordpress_20250130_030000.sql.gz
        └── ...
```

### Restaurer un backup

**PostgreSQL :**

```bash
# Décompresser et restaurer
gunzip -c backups/postgres/myapp_db/myapp_db_20250131_020000.sql.gz | \
  psql -h localhost -U postgres -d myapp_db
```

**MariaDB :**

```bash
# Décompresser et restaurer
gunzip -c backups/mariadb/wordpress/wordpress_20250131_030000.sql.gz | \
  mysql -h localhost -u root -p wordpress
```

### Tester un backup manuellement

Vous pouvez exécuter un backup manuellement sans attendre le cron :

```bash
docker exec db-backup /scripts/backup.sh \
  postgres \
  postgres-db \
  5432 \
  myapp_db \
  postgres \
  postgres_password \
  14
```

## Sécurité

### Gestion des mots de passe

**Support des caractères spéciaux :**
Le système gère automatiquement les mots de passe complexes contenant tous types de caractères spéciaux :
- Symboles : `!`, `@`, `#`, `$`, `%`, `^`, `&`, `*`, `(`, `)`, `-`, `_`, `+`, `=`
- Espaces (bien que déconseillés)
- Caractères Unicode

**Fonctionnement :**
- Pour **PostgreSQL** : utilise la variable d'environnement `PGPASSWORD` (méthode sécurisée recommandée)
- Pour **MariaDB/MySQL** : utilise la variable d'environnement `MYSQL_PWD` (évite l'exposition en ligne de commande)
- Les mots de passe sont automatiquement échappés avec `printf %q` pour être passés en toute sécurité à travers le système cron

### Bonnes pratiques

1. **Permissions des fichiers** : Assurez-vous que `backups.conf` a des permissions restrictives car il contient des mots de passe :

```bash
chmod 600 backups.conf
```

2. **Utilisateurs de backup dédiés** : Créez des utilisateurs avec privilèges minimaux pour les backups :

**PostgreSQL :**
```sql
CREATE USER backup_user WITH PASSWORD 'secure_password';
GRANT CONNECT ON DATABASE myapp_db TO backup_user;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO backup_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO backup_user;
```

**MariaDB :**
```sql
CREATE USER 'backup_user'@'%' IDENTIFIED BY 'secure_password';
GRANT SELECT, LOCK TABLES, SHOW VIEW, EVENT, TRIGGER ON myapp_db.* TO 'backup_user'@'%';
FLUSH PRIVILEGES;
```

3. **Stockage des backups** : Considérez de monter un volume chiffré pour `/backups`

4. **Backups externes** : Synchronisez régulièrement les backups vers un stockage externe (S3, NAS, etc.)

## Monitoring

### Vérifier l'état du service

```bash
docker-compose ps
```

### Consulter les logs en temps réel

```bash
docker-compose logs -f db-backup
```

### Vérifier les derniers backups

```bash
find backups -name "*.sql.gz" -type f -mtime -1 -ls
```

## Dépannage

### Le container ne démarre pas

Vérifiez que le fichier `backups.conf` existe :

```bash
docker-compose logs db-backup
```

### Les backups ne s'exécutent pas

1. Vérifiez la configuration cron :

```bash
docker exec db-backup cat /etc/cron.d/db-backups
```

2. Vérifiez que cron est en cours d'exécution :

```bash
docker exec db-backup ps aux | grep cron
```

3. Testez la connexion à la base de données :

```bash
# PostgreSQL
docker exec db-backup pg_dump --version
docker exec db-backup psql -h postgres-db -U postgres -d myapp_db -c "SELECT 1"

# MariaDB
docker exec db-backup mysqldump --version
docker exec db-backup mysql -h mariadb-db -u wp_user -p'wp_password' -e "SELECT 1"
```

### Les anciens backups ne sont pas supprimés

Vérifiez que `RETENTION_DAYS` est bien défini dans votre configuration et que la valeur est un nombre positif.

## Personnalisation

### Modifier le fuseau horaire

Ajoutez dans le Dockerfile :

```dockerfile
ENV TZ=Europe/Paris
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
```

### Ajouter des notifications

Modifiez `scripts/backup.sh` pour envoyer des notifications par email ou webhook en cas de succès ou d'échec.

### Changer le format de compression

Remplacez `gzip` par `bzip2` ou `xz` dans `scripts/backup.sh` pour une meilleure compression.

## CI/CD avec GitHub Actions

Ce projet inclut un workflow GitHub Actions (`.github/workflows/docker-build.yml`) qui build et publie automatiquement l'image Docker sur GitHub Container Registry.

### Déclenchement du build

Le workflow se déclenche automatiquement sur :
- Push sur la branche `main` ou `master`
- Création d'un tag (ex: `v1.0.0`)
- Pull requests
- Déclenchement manuel via l'interface GitHub

### Configuration requise

1. **Activer GitHub Container Registry** : Aucune configuration nécessaire, c'est activé par défaut pour tous les repositories GitHub.

2. **Rendre l'image publique** (optionnel) :
   - Allez sur `https://github.com/users/VOTRE_USERNAME/packages/container/REPO_NAME/settings`
   - Changez la visibilité de "Private" à "Public"

### Créer une release

Pour créer une nouvelle version taggée :

```bash
git tag -a v1.0.0 -m "Release version 1.0.0"
git push origin v1.0.0
```

Cela créera automatiquement les tags Docker suivants :
- `ghcr.io/username/repository:v1.0.0`
- `ghcr.io/username/repository:1.0`
- `ghcr.io/username/repository:1`
- `ghcr.io/username/repository:latest`

### Vérifier le build

1. Allez dans l'onglet "Actions" de votre repository GitHub
2. Sélectionnez le workflow "Build and Push Docker Image"
3. Vérifiez que le build a réussi
4. L'image sera disponible dans la section "Packages" de votre repository

### Utiliser l'image buildée

Une fois l'image publiée, remplacez dans votre `docker-compose.yml` :

```yaml
# Avant (build local)
services:
  db-backup:
    build: .

# Après (utiliser l'image pré-buildée)
services:
  db-backup:
    image: ghcr.io/username/repository:latest
```

## Licence

MIT
