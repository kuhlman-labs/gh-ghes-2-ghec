# CSS Build Setup and Integration

## Overview

This document explains the CSS build process integration with the Go application build pipeline.

## Why Build CSS?

Your Go application serves **built/concatenated CSS files** (`/static/css/dist/styles.css`) rather than individual source files. The CSS build process:

1. **Lints** all CSS files for code quality
2. **Concatenates** multiple CSS files in the correct order
3. **Minifies** the output for production performance
4. **Optimizes** for browser delivery

## Build Process

### Source Files (in order)
```
static/css/_variables.css    # Design tokens and CSS custom properties
static/css/_base.css         # Reset, typography, foundational styles  
static/css/_layout.css       # Page structure and layout components
static/css/_components.css   # Reusable UI components
static/css/charts.css        # Chart-specific styles
static/css/_utilities.css    # Atomic helper classes
static/css/wizard.css        # Migration wizard styles
```

### Output Files
```
static/css/dist/styles.css     # Concatenated, linted CSS (109KB)
static/css/dist/styles.min.css # Minified version (80KB, 29% smaller)
```

## Integration Points

### 1. Makefile Targets

```bash
# CSS-specific targets
make css-deps     # Install Node.js dependencies
make css-build    # Build and minify CSS files  
make css-lint     # Lint CSS files only
make css-clean    # Clean CSS build artifacts

# Integrated targets
make build        # Now includes CSS build
make clean        # Now includes CSS cleanup
make all          # Full pipeline: fmt, lint, test, css-build, build
```

### 2. GitHub Actions Workflows

#### Dedicated CSS Workflow (`.github/workflows/css.yml`)
- Triggers on CSS file changes
- Runs CSS linting and building
- Uploads build artifacts
- Provides build statistics

#### Main Go Workflow (`.github/workflows/go.yaml`)
- Now includes CSS building before Go build
- Caches Node.js dependencies
- Runs CSS linting alongside Go linting

#### Docker Workflow (`.github/workflows/docker-publish.yml`)
- Builds CSS before Docker image creation
- Ensures built CSS is included in container

### 3. Docker Integration

The `Dockerfile` now:
- Installs Node.js and npm
- Copies CSS dependencies separately for better caching
- Builds CSS during Docker image creation
- Includes built CSS files in the final image

## Development Workflow

### Local Development

```bash
# Full development build
make dev

# CSS-only changes
make css-build

# Watch CSS changes (if you want continuous building)
cd static/css && npm run watch
```

### Git Workflow

- **Source CSS files**: Tracked in git
- **Built CSS files**: Now ignored (added to `.gitignore`)
- **Dependencies**: `node_modules/` ignored
- **Build artifacts**: Generated during CI/CD

## Best Practices

### 1. **Always build CSS for production**
The Go application expects built CSS files to exist. Never deploy without building CSS.

### 2. **CSS changes require rebuilding**
After modifying any CSS source file, run `make css-build` or `make build`.

### 3. **Linting is enforced**
CSS must pass linting before building. Fix linting errors with:
```bash
cd static/css && npm run lint:fix
```

### 4. **Use the design system**
Follow the established CSS architecture:
- Variables in `_variables.css`
- Components follow BEM methodology
- Use utility classes for spacing/layout

## Troubleshooting

### Build Failures

```bash
# Clean and rebuild everything
make clean && make build

# CSS-specific issues
make css-clean && make css-build

# Check CSS syntax
make css-lint
```

### Missing Dependencies

```bash
# Reinstall CSS dependencies
cd static/css && npm install
```

### Docker Build Issues

```bash
# Ensure CSS is built before Docker
make css-build
docker build .
```

## Performance Impact

- **Development**: CSS build adds ~3-5 seconds to build time
- **CI/CD**: Cached dependencies minimize impact
- **Production**: 29% smaller CSS files improve page load times
- **Browser**: Single CSS file reduces HTTP requests

## File Structure

```
static/css/
├── package.json           # Node.js dependencies and scripts
├── package-lock.json      # Dependency lock file
├── .stylelintrc.json     # CSS linting configuration
├── dist/                 # Built CSS files (ignored in git)
│   ├── styles.css        # Concatenated CSS
│   └── styles.min.css    # Minified CSS
├── _variables.css        # Design tokens
├── _base.css            # Base styles
├── _layout.css          # Layout components
├── _components.css      # UI components
├── _utilities.css       # Utility classes
├── charts.css           # Chart styles
└── wizard.css           # Wizard styles
```

## Next Steps

1. **Monitor build times** in CI/CD
2. **Consider CSS preprocessing** (Sass/PostCSS) if needed
3. **Implement CSS purging** for even smaller bundles
4. **Add CSS testing** for critical styles
5. **Set up CSS performance budgets** 