#!/usr/bin/env sh
set -eu

HOST="${TFE_HOST_OVERRIDE:-$(awk -F'"' '/^hostname/{print $2}' /root/.tfx.hcl)}"
ORG="$(awk -F'"' '/^defaultOrganization/{print $2}' /root/.tfx.hcl)"
TOKEN="$(awk -F'"' '/^token/{print $2}' /root/.tfx.hcl)"
API="https://${HOST}/api/v2"
CT="application/vnd.api+json"

HOST_NAME="${HOST%%:*}"
HOST_PORT="${HOST##*:}"
HOST_IP="$(getent hosts "${HOST_NAME}" | awk '{print $1}' | head -n1 || true)"

CURL_RESOLVE_ARGS=""
if [ -n "${HOST_IP}" ]; then
  CURL_RESOLVE_ARGS="--resolve ${HOST_NAME}:${HOST_PORT}:${HOST_IP}"
fi

curl_api() {
  # shellcheck disable=SC2086
  curl -ksS ${CURL_RESOLVE_ARGS} "$@"
}

# If the default hostname from .tfx.hcl is not reachable from inside the helper
# network, fall back to the internal TFE service endpoint and recompute resolve args.
if ! curl_api -o /dev/null "https://${HOST}/_health_check"; then
  HOST="hal-tfe:8443"
  API="https://${HOST}/api/v2"
  HOST_NAME="${HOST%%:*}"
  HOST_PORT="${HOST##*:}"
  HOST_IP="$(getent hosts "${HOST_NAME}" | awk '{print $1}' | head -n1 || true)"
  CURL_RESOLVE_ARGS=""
  if [ -n "${HOST_IP}" ]; then
    CURL_RESOLVE_ARGS="--resolve ${HOST_NAME}:${HOST_PORT}:${HOST_IP}"
  fi
fi

mkdir -p /workspaces

MANAGED_WS_FILE="/root/.hal-tfe-cli-managed-workspaces"
MANAGED_REPOS_FILE="/root/.hal-tfe-cli-managed-repos"

list_projects() {
  curl_api -o /tmp/projects.json \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: ${CT}" \
    "${API}/organizations/${ORG}/projects" >/dev/null
}

extract_project_id_from_list() {
  project_name="$1"
  tr '\n' ' ' < /tmp/projects.json | sed 's/},{/}\
{/g' | grep '"type":"projects"' | grep "\"name\":\"${project_name}\"" | head -n1 | grep -o '"id":"[^"]*"' | head -n1 | cut -d'"' -f4
}

ensure_project() {
  project_name="$1"

  list_projects
  project_id="$(extract_project_id_from_list "${project_name}")"
  if [ -n "${project_id}" ]; then
    printf '%s' "${project_id}"
    return 0
  fi

  cat > /tmp/project_create.json <<EOF
{
  "data": {
    "type": "projects",
    "attributes": {
      "name": "${project_name}"
    }
  }
}
EOF

  code="$(curl_api -o /tmp/project_create_resp.json -w '%{http_code}' \
    -X POST \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: ${CT}" \
    -H "Content-Type: ${CT}" \
    --data @/tmp/project_create.json \
    "${API}/organizations/${ORG}/projects")"

  if [ "$code" != "201" ] && [ "$code" != "200" ]; then
    echo "[project] create failed for ${project_name} (status ${code})"
    cat /tmp/project_create_resp.json
    exit 1
  fi

  grep -o '"id":"[^"]*"' /tmp/project_create_resp.json | head -n1 | cut -d'"' -f4
}

ensure_ws() {
  ws="$1"
  project_id="$2"
  code="$(curl_api -o /tmp/ws_get.json -w '%{http_code}' \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: ${CT}" \
    "${API}/organizations/${ORG}/workspaces/${ws}")"

  if [ "$code" = "200" ]; then
    workspace_id="$(grep -o '"id":"[^"]*"' /tmp/ws_get.json | head -n1 | cut -d'"' -f4)"
    cat > /tmp/ws_patch.json <<EOF
{
  "data": {
    "id": "${workspace_id}",
    "type": "workspaces",
    "relationships": {
      "project": {
        "data": {
          "id": "${project_id}",
          "type": "projects"
        }
      }
    }
  }
}
EOF

    code="$(curl_api -o /tmp/ws_patch_resp.json -w '%{http_code}' \
      -X PATCH \
      -H "Authorization: Bearer ${TOKEN}" \
      -H "Accept: ${CT}" \
      -H "Content-Type: ${CT}" \
      --data @/tmp/ws_patch.json \
      "${API}/workspaces/${workspace_id}")"

    if [ "$code" != "200" ]; then
      echo "[ws] move failed for ${ws} (status ${code})"
      cat /tmp/ws_patch_resp.json
      exit 1
    fi

    echo "[ws] exists: ${ws}"
    return 0
  fi

  if [ "$code" != "404" ]; then
    echo "[ws] lookup failed for ${ws} (status ${code})"
    cat /tmp/ws_get.json
    exit 1
  fi

  cat > /tmp/ws_create.json <<EOF
{
  "data": {
    "type": "workspaces",
    "attributes": {
      "name": "${ws}",
      "auto-apply": true,
      "execution-mode": "remote"
    },
    "relationships": {
      "project": {
        "data": {
          "id": "${project_id}",
          "type": "projects"
        }
      }
    }
  }
}
EOF

  code="$(curl_api -o /tmp/ws_create_resp.json -w '%{http_code}' \
    -X POST \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: ${CT}" \
    -H "Content-Type: ${CT}" \
    --data @/tmp/ws_create.json \
    "${API}/organizations/${ORG}/workspaces")"

  if [ "$code" != "201" ] && [ "$code" != "200" ]; then
    echo "[ws] create failed for ${ws} (status ${code})"
    cat /tmp/ws_create_resp.json
    exit 1
  fi

  echo "[ws] created: ${ws}"
}

seed_repo() {
  repo="$1"
  ws="$2"
  theme="$3"
  dad_joke="$4"

  mkdir -p "/workspaces/${repo}"
  cat > "/workspaces/${repo}/main.tf" <<EOF
terraform {
  required_version = ">= 1.6.0"

  cloud {
    hostname     = "${HOST}"
    organization = "${ORG}"

    workspaces {
      name = "${ws}"
    }
  }
}

variable "revision" {
  type    = string
  default = "r0"
}

locals {
  repo_name = "${repo}"
  theme     = "${theme}"
  dad_joke  = "${dad_joke}"
}

output "repo" {
  value = local.repo_name
}

output "theme" {
  value = local.theme
}

output "dad_joke" {
  value = local.dad_joke
}

output "revision" {
  value = var.revision
}
EOF

  (cd "/workspaces/${repo}" && terraform init -input=false >/tmp/${repo}_init.log)
  echo "[repo] ready: /workspaces/${repo} -> ${ws}"
}

run_apply() {
  repo="$1"
  tag="$2"
  (cd "/workspaces/${repo}" && terraform apply -auto-approve -input=false -var "revision=${tag}" >/tmp/${repo}_${tag}_apply.log)
  echo "[run] apply ${repo} ${tag}"
}

run_plan() {
  repo="$1"
  tag="$2"
  (cd "/workspaces/${repo}" && terraform plan -input=false -var "revision=${tag}" >/tmp/${repo}_${tag}_plan.log)
  echo "[run] plan  ${repo} ${tag}"
}

printf '%s\n' \
  'hal-lucinated' \
  'hal-ogen' \
  'hal-lelujah' \
  'hal-oween' \
  'hal-ibut' > "${MANAGED_WS_FILE}"

printf '%s\n' \
  'hal-lucinated' \
  'hal-ogen' \
  'hal-lelujah' \
  'hal-oween' \
  'hal-ibut' > "${MANAGED_REPOS_FILE}"

DAVE_ID="$(ensure_project 'Dave')"
FRANK_ID="$(ensure_project 'Frank')"

ensure_ws "hal-lucinated" "${DAVE_ID}"
seed_repo "hal-lucinated" "hal-lucinated" "mushrooms in the forest" "These runs are fun-guys." 

ensure_ws "hal-ogen" "${FRANK_ID}"
seed_repo "hal-ogen" "hal-ogen" "periodic table lighting" "This workspace has brilliant chemistry." 

ensure_ws "hal-lelujah" "${DAVE_ID}"
seed_repo "hal-lelujah" "hal-lelujah" "choir practice and cloud harmony" "Even the plans sing in four-part harmony." 

ensure_ws "hal-oween" "${FRANK_ID}"
seed_repo "hal-oween" "hal-oween" "pumpkins, ghosts, and spooky drift" "This stack is haunted by state spirits." 

ensure_ws "hal-ibut" "${DAVE_ID}"
seed_repo "hal-ibut" "hal-ibut" "deep-sea fish operations" "Something smells fishy, but the plan is clean." 

run_apply hal-lucinated fungi-a1
run_apply hal-lucinated fungi-a2
run_apply hal-lucinated fungi-a3
run_apply hal-lucinated fungi-a4
run_apply hal-lucinated fungi-a5

run_plan hal-ogen element-p1
run_plan hal-ogen element-p2
run_plan hal-ogen element-p3

run_apply hal-lelujah choir-a1
run_plan hal-lelujah choir-p1
run_apply hal-lelujah choir-a2

run_plan hal-oween spooky-p1
run_apply hal-oween spooky-a1

echo ""
echo "Scenario complete."
echo "Repos under /workspaces:"
for repo in hal-lucinated hal-ogen hal-lelujah hal-oween hal-ibut; do
  [ -d "/workspaces/${repo}" ] && echo "${repo}"
done

echo ""
echo "Workspace map:"
echo "  hal-lucinated -> hal-lucinated (Dave, 5 applies, mushroom jokes)"
echo "  hal-ogen      -> hal-ogen      (Frank, 3 plans, chemistry theme)"
echo "  hal-lelujah   -> hal-lelujah   (Dave, 2 applies, 1 plan, choir theme)"
echo "  hal-oween     -> hal-oween     (Frank, 1 plan, 1 apply, spooky theme)"
echo "  hal-ibut      -> hal-ibut      (Dave, 0 runs, fish theme)"
