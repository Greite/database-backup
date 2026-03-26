# GitHub Actions & Container Registry Setup

This guide explains how to configure your GitHub repository to automatically build and publish the Docker image.

## Prerequisites

- A GitHub account
- A GitHub repository containing this project
- Git installed locally

## Step 1: Push Your Code to GitHub

If not already done, create a GitHub repository and push your code:

```bash
# Initialize the git repository
git init

# Add all files
git add .

# Create the first commit
git commit -m "Initial commit: Database backup container"

# Add your GitHub repository as a remote
git remote add origin https://github.com/greite/database-backup.git

# Push to GitHub
git branch -M main
git push -u origin main
```

## Step 2: Verify the GitHub Actions Workflow

1. Go to your GitHub repository in a browser
2. Click the **Actions** tab
3. You should see the "Build and Push Docker Image" workflow running or completed
4. Click on it to view the build details

The workflow triggers automatically when you push to the `main` branch.

## Step 3: Verify the Built Image

1. On your GitHub repository, go to the **Packages** tab (in the right-side navigation bar)
2. You should see a package named after your repository
3. Click on it to view details and available tags

## Step 4: Make the Image Public (optional)

By default, the image is private. To make it public:

1. Go to the package page: `https://github.com/users/greite/packages/container/database-backup/settings`
2. Scroll down to the "Danger Zone" section
3. Click "Change visibility"
4. Select "Public"
5. Confirm by typing the package name

## Step 5: Use the Image

### Pull the image

For a public image:
```bash
docker pull ghcr.io/greite/database-backup:latest
```

For a private image, you must authenticate first:
```bash
# Create a Personal Access Token (classic) with the 'read:packages' scope
# Go to: https://github.com/settings/tokens

# Authenticate
echo "YOUR_TOKEN" | docker login ghcr.io -u greite --password-stdin

# Then pull the image
docker pull ghcr.io/greite/database-backup:latest
```

### Usage in compose.yml

Update your `compose.yml` to use the pre-built image:

```yaml
services:
  db-backup:
    image: ghcr.io/greite/database-backup:latest
    # ... rest of the configuration
```

## Step 6: Create Versioned Releases

To create a tagged version (e.g., v1.0.0):

```bash
# Create an annotated tag
git tag -a v1.0.0 -m "Release version 1.0.0"

# Push the tag to GitHub
git push origin v1.0.0
```

This will automatically trigger the workflow and create the following Docker tags:
- `ghcr.io/greite/database-backup:latest`
- `ghcr.io/greite/database-backup:v1.0.0`
- `ghcr.io/greite/database-backup:1.0`
- `ghcr.io/greite/database-backup:1`

## How the Workflows Work

### Release Build (`docker-build.yml`)

Triggered on version tags (`v*.*.*`) or manual dispatch. Builds and pushes a multi-architecture Docker image with semantic versioning tags.

### Base Image Update Check (`base-image-check.yml`)

Runs **4 times daily** (00:00, 06:00, 12:00, 18:00 UTC) to detect updates to the `debian:trixie-slim` base image. If a new version is found, the image is automatically rebuilt and pushed to keep it up to date with the latest security patches.

### Automatic Tags

The release workflow automatically creates multiple tags:

| Event | Tags Created |
|-------|--------------|
| Tag `v1.2.3` | `v1.2.3`, `1.2`, `1`, `latest` |
| Base image update | `latest` |

## Customizing the Workflow

### Change the registry

To use Docker Hub instead of ghcr.io, edit `.github/workflows/docker-build.yml`:

```yaml
env:
  REGISTRY: docker.io
  IMAGE_NAME: greite/database-backup
```

And add your Docker Hub credentials to GitHub secrets:
1. Go to Settings > Secrets and variables > Actions
2. Add `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`
3. Update the login step:

```yaml
- name: Log in to Docker Hub
  uses: docker/login-action@v3
  with:
    username: ${{ secrets.DOCKERHUB_USERNAME }}
    password: ${{ secrets.DOCKERHUB_TOKEN }}
```

### Disable ARM64 builds

To build only for amd64 (faster), change:

```yaml
platforms: linux/amd64  # Remove linux/arm64
```

## Troubleshooting

### Workflow fails

1. Check the logs in the Actions tab
2. Ensure permissions are correct in `.github/workflows/docker-build.yml`
3. Verify the Dockerfile is valid: `docker build -t test .`

### Image doesn't appear in Packages

1. Verify the workflow ran successfully
2. Make sure you're logged into the correct GitHub account
3. Private images are only visible to repository members

### Cannot pull a private image

1. Create a Personal Access Token with the `read:packages` scope
2. Authenticate: `echo "TOKEN" | docker login ghcr.io -u USERNAME --password-stdin`
3. Try the pull again

## Resources

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [GitHub Container Registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [Docker Build Push Action](https://github.com/docker/build-push-action)
