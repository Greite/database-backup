# Configuration GitHub Actions et Container Registry

Ce guide vous explique comment configurer votre repository GitHub pour builder et publier automatiquement l'image Docker.

## Prérequis

- Un compte GitHub
- Un repository GitHub contenant ce projet
- Git installé localement

## Étape 1 : Pousser le code sur GitHub

Si ce n'est pas encore fait, créez un repository GitHub et poussez votre code :

```bash
# Initialisez le repository git
git init

# Ajoutez tous les fichiers
git add .

# Créez le premier commit
git commit -m "Initial commit: Database backup container"

# Ajoutez votre repository GitHub comme remote
git remote add origin https://github.com/greite/database-backup.git

# Poussez sur GitHub
git branch -M main
git push -u origin main
```

## Étape 2 : Vérifier le workflow GitHub Actions

1. Allez sur votre repository GitHub dans un navigateur
2. Cliquez sur l'onglet **Actions**
3. Vous devriez voir le workflow "Build and Push Docker Image" en cours ou terminé
4. Cliquez dessus pour voir les détails du build

Le workflow se déclenche automatiquement car vous avez poussé sur la branche `main`.

## Étape 3 : Vérifier l'image buildée

1. Sur votre repository GitHub, allez dans l'onglet **Packages** (dans la barre de navigation de droite)
2. Vous devriez voir un package nommé comme votre repository
3. Cliquez dessus pour voir les détails et les tags disponibles

## Étape 4 : Rendre l'image publique (optionnel)

Par défaut, l'image est privée. Pour la rendre publique :

1. Allez sur la page du package : `https://github.com/users/greite/packages/container/database-backup/settings`
2. Scrollez jusqu'à la section "Danger Zone"
3. Cliquez sur "Change visibility"
4. Sélectionnez "Public"
5. Confirmez en tapant le nom du package

## Étape 5 : Utiliser l'image

### Pull de l'image

Pour une image publique :
```bash
docker pull ghcr.io/greite/database-backup:latest
```

Pour une image privée, vous devez d'abord vous authentifier :
```bash
# Créez un Personal Access Token (classic) avec le scope 'read:packages'
# Allez sur : https://github.com/settings/tokens

# Authentifiez-vous
echo "VOTRE_TOKEN" | docker login ghcr.io -u greite --password-stdin

# Ensuite pull l'image
docker pull ghcr.io/greite/database-backup:latest
```

### Utilisation dans docker-compose.yml

Modifiez votre `docker-compose.yml` pour utiliser l'image pré-buildée :

```yaml
version: '3.8'

services:
  db-backup:
    image: ghcr.io/greite/database-backup:latest
    # ... reste de la configuration
```

## Étape 6 : Créer des releases versionnées

Pour créer une version taggée (ex: v1.0.0) :

```bash
# Créez un tag annoté
git tag -a v1.0.0 -m "Release version 1.0.0"

# Poussez le tag sur GitHub
git push origin v1.0.0
```

Cela déclenchera automatiquement le workflow et créera les tags Docker suivants :
- `ghcr.io/greite/database-backup:latest`
- `ghcr.io/greite/database-backup:v1.0.0`
- `ghcr.io/greite/database-backup:1.0`
- `ghcr.io/greite/database-backup:1`

## Fonctionnement du workflow

Le workflow `.github/workflows/docker-build.yml` :

1. **Se déclenche** sur :
   - Push sur `main` ou `master`
   - Création d'un tag `v*`
   - Pull requests
   - Déclenchement manuel

2. **Build l'image** avec :
   - Support multi-architecture (amd64, arm64)
   - Cache optimisé pour des builds plus rapides
   - Tags automatiques basés sur les branches/tags git

3. **Publie l'image** sur GitHub Container Registry (ghcr.io)

## Tags automatiques

Le workflow crée automatiquement plusieurs tags :

| Événement | Tags créés |
|-----------|------------|
| Push sur `main` | `latest`, `main-abc1234` |
| Tag `v1.2.3` | `v1.2.3`, `1.2`, `1`, `latest` |
| Push sur branche `dev` | `dev`, `dev-abc1234` |
| Pull request #42 | `pr-42` |

## Personnalisation du workflow

### Changer le registry

Pour utiliser Docker Hub au lieu de ghcr.io, modifiez `.github/workflows/docker-build.yml` :

```yaml
env:
  REGISTRY: docker.io
  IMAGE_NAME: greite/database-backup
```

Et ajoutez vos credentials Docker Hub dans les secrets GitHub :
1. Allez dans Settings > Secrets and variables > Actions
2. Ajoutez `DOCKERHUB_USERNAME` et `DOCKERHUB_TOKEN`
3. Modifiez la step de login :

```yaml
- name: Log in to Docker Hub
  uses: docker/login-action@v3
  with:
    username: ${{ secrets.DOCKERHUB_USERNAME }}
    password: ${{ secrets.DOCKERHUB_TOKEN }}
```

### Désactiver les builds ARM64

Si vous ne voulez builder que pour amd64 (plus rapide), modifiez :

```yaml
platforms: linux/amd64  # Enlevez linux/arm64
```

## Dépannage

### Le workflow échoue

1. Vérifiez les logs dans l'onglet Actions
2. Assurez-vous que les permissions sont correctes dans `.github/workflows/docker-build.yml`
3. Vérifiez que le Dockerfile est valide : `docker build -t test .`

### L'image n'apparaît pas dans Packages

1. Vérifiez que le workflow s'est exécuté avec succès
2. Assurez-vous que vous êtes connecté au bon compte GitHub
3. Les images privées n'apparaissent que pour les membres du repository

### Impossible de pull l'image privée

1. Créez un Personal Access Token avec le scope `read:packages`
2. Authentifiez-vous : `echo "TOKEN" | docker login ghcr.io -u USERNAME --password-stdin`
3. Essayez à nouveau le pull

## Ressources

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [GitHub Container Registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [Docker Build Push Action](https://github.com/docker/build-push-action)
