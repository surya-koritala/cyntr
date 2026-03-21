---
name: onboarding-guide
description: Generate personalized onboarding checklists and project walkthroughs for new team members
version: 1.0.0
author: cyntr
tools:
  - name: file_read
  - name: file_search
  - name: shell_exec
  - name: file_write
  - name: knowledge_search
---

# New Team Member Onboarding Guide

Walk a new team member through the project structure, architecture, tech stack,
coding conventions, and key resources. Produce a personalized onboarding
checklist tailored to their role.

## Prerequisites

- Must be run inside a project repository.
- The user should specify the new team member's name and role (e.g., backend
  engineer, frontend engineer, DevOps, QA, data engineer).
- Existing onboarding documentation in the knowledge base will be incorporated
  if available.

## Step 1: Search for Existing Onboarding Docs

Check the knowledge base for onboarding materials:

```
knowledge_search("onboarding" OR "getting started" OR "new developer" OR "setup guide")
```

Also search the repository for common onboarding file names:

```
file_search("ONBOARDING*")
file_search("GETTING_STARTED*")
file_search("CONTRIBUTING*")
file_search("SETUP*")
file_search("README*")
file_search("docs/onboarding*")
file_search("docs/getting-started*")
file_search(".github/CONTRIBUTING*")
```

Read any files found. Extract:
- Setup instructions
- Required tools and versions
- Environment configuration steps
- Common workflows

## Step 2: Analyze Project Structure

Map the repository structure:

```
# Top-level directory listing
ls -la

# Directory tree (depth 3, exclude common noise)
find . -maxdepth 3 -type d \
  -not -path '*/node_modules/*' \
  -not -path '*/.git/*' \
  -not -path '*/vendor/*' \
  -not -path '*/__pycache__/*' \
  -not -path '*/dist/*' \
  -not -path '*/build/*' \
  -not -path '*/.next/*' \
  -not -path '*/target/*' \
  | sort | head -80

# Count files by extension to understand language mix
find . -type f -not -path '*/node_modules/*' -not -path '*/.git/*' \
  | sed 's/.*\.//' | sort | uniq -c | sort -rn | head -15
```

For each major directory, provide a one-line description of its purpose based
on its name and contents.

## Step 3: Identify Tech Stack

Detect the tech stack by examining configuration files:

```
# Package managers / dependency files
ls package.json Cargo.toml go.mod requirements.txt Pipfile Gemfile \
   pom.xml build.gradle setup.py pyproject.toml composer.json \
   mix.exs pubspec.yaml 2>/dev/null

# Frameworks
ls next.config.* nuxt.config.* angular.json vue.config.* \
   django-admin.py manage.py config/routes.rb \
   Dockerfile docker-compose.yml k8s/ terraform/ 2>/dev/null

# CI/CD
ls .github/workflows/*.yml .gitlab-ci.yml Jenkinsfile \
   .circleci/config.yml .travis.yml 2>/dev/null
```

For each detected component, read its config file to determine:
- Language version
- Framework version
- Key dependencies
- Build/run commands

## Step 4: Extract Architecture Overview

Look for architecture documentation:

```
file_search("ARCHITECTURE*")
file_search("docs/architecture*")
file_search("docs/design*")
file_search("ADR*")
file_search("docs/adr*")
```

If no architecture docs exist, infer the architecture:

1. **Entry points**: Find `main`, `index`, `app`, `server` files.
   ```
   file_search("main.*")
   file_search("index.*")
   file_search("app.*")
   file_search("server.*")
   ```

2. **API layer**: Look for route definitions.
   ```
   file_search("*routes*")
   file_search("*controllers*")
   file_search("*handlers*")
   file_search("*api*")
   ```

3. **Data layer**: Look for models, schemas, migrations.
   ```
   file_search("*models*")
   file_search("*schema*")
   file_search("*migrations*")
   ```

4. **Configuration**: Look for environment and config files.
   ```
   file_search("*.env.example")
   file_search("config/*")
   ```

Produce a brief narrative: "This is a [monolith/microservice/monorepo] using
[framework] with [database]. The main entry point is [file]. Requests flow
from [route layer] to [service layer] to [data layer]."

## Step 5: Identify Coding Conventions

Detect coding standards from config files:

```
# Linting
ls .eslintrc* .pylintrc .rubocop.yml .golangci.yml rustfmt.toml \
   .prettierrc* .editorconfig tslint.json biome.json 2>/dev/null

# Formatting
ls .prettierrc* .clang-format .editorconfig rustfmt.toml 2>/dev/null

# Git hooks
ls .husky/* .pre-commit-config.yaml .git/hooks/* 2>/dev/null

# Type checking
ls tsconfig.json mypy.ini .mypy.ini pyright*.json 2>/dev/null
```

Read the detected config files and summarize the key conventions:
- Indentation style (tabs vs. spaces, width)
- Naming conventions
- Import ordering
- Commit message format (check for commitlint, conventional commits)
- Branch naming conventions (check CONTRIBUTING or similar docs)
- Test file naming and location conventions

## Step 6: Map Key Files

Identify the most important files a new team member should read:

1. **Configuration files**: Read these to understand how the project is
   configured.
2. **Entry point files**: Read these to understand how the application starts.
3. **Core business logic**: Identify the most-changed files (these are the
   "heart" of the system).
   ```
   git log --since="90 days ago" --pretty=format: --name-only | sort | uniq -c | sort -rn | head -10
   ```
4. **Test examples**: Find a well-written test to understand testing patterns.
   ```
   file_search("*test*")
   file_search("*spec*")
   ```
   Read one representative test file.

For each key file, provide:
- File path
- What it does (one sentence)
- Why it matters for onboarding

## Step 7: Identify Key Resources

Gather links and references:

```
# Check for wiki links in README
file_read("README.md") | grep -i "wiki\|docs\|documentation\|confluence\|notion"

# Check for tool configuration
ls .tool-versions .node-version .python-version .ruby-version 2>/dev/null
```

Compile a list of:
- Internal documentation links (from README, CONTRIBUTING, etc.)
- External tool URLs (CI/CD dashboards, monitoring, staging environments)
- Communication channels (from CONTRIBUTING or similar docs)
- Required access and permissions (from onboarding docs)

## Step 8: Generate Personalized Checklist

Based on the new team member's role, generate a role-specific checklist:

```
# Onboarding Guide: <Project Name>
**Prepared for:** <Name> (<Role>)
**Generated:** <current date>

## Welcome
<1-2 sentence project description from README>

## Project Overview
- **Language(s):** <detected languages>
- **Framework(s):** <detected frameworks>
- **Database:** <detected database>
- **Infrastructure:** <detected infra tools>
- **CI/CD:** <detected pipeline>

## Architecture
<Brief architecture narrative from Step 4>

## Project Structure
```
<directory tree with annotations>
```

## Key Files to Read First
| # | File | Purpose | Priority |
|---|------|---------|----------|
| 1 | <path> | <description> | Must read |
| 2 | <path> | <description> | Must read |
| 3 | <path> | <description> | Should read |
...

## Setup Checklist
- [ ] Clone the repository
- [ ] Install required tools: <list with versions>
- [ ] Copy `.env.example` to `.env` and configure
- [ ] Install dependencies: `<install command>`
- [ ] Run the project locally: `<run command>`
- [ ] Run tests: `<test command>`
- [ ] Verify linting passes: `<lint command>`

## Coding Conventions
- <Convention 1>
- <Convention 2>
...

## Development Workflow
1. Create a branch: `<branch naming convention>`
2. Make changes and commit: `<commit convention>`
3. Push and create a PR: `<PR process>`
4. Code review: `<review process>`
5. Merge: `<merge strategy>`

## Role-Specific Tasks

### Week 1: Get Oriented
- [ ] Complete setup checklist above
- [ ] Read key files listed above
- [ ] <Role-specific task: e.g., "Run the test suite and understand coverage">
- [ ] Pair with <team lead> on a current task
- [ ] Make a small "good first issue" contribution

### Week 2: Go Deeper
- [ ] <Role-specific deeper task>
- [ ] <Understand a specific subsystem>
- [ ] Attend team standup and retro

### Week 3-4: Contribute
- [ ] Pick up a regular-sized ticket independently
- [ ] <Role-specific contribution goal>

## Key Resources
| Resource | URL/Location |
|----------|-------------|
<rows>

## Key Contacts
| Role | Name | Contact |
|------|------|---------|
<rows if discoverable from git history or docs>

---
*Generated by cyntr/onboarding-guide v1.0.0*
```

## Step 9: Output

Write the guide to file (suggested: `onboarding-<name>-<date>.md`).
Also return the full guide as the response.

If `knowledge_store` is available, store the generated guide so it can be
reused and updated for future onboardees.
