.PHONY: install-postgres start-postgres stop-postgres create-database create-user grant-permissions start-postgres-container wait-postgres-container stop-postgres-container delete-postgres-container cleanup-postgres-container

UNAME := $(shell uname)

POSTGRES_VERSION ?= 18

ifeq (${UNAME}, Darwin)
install-postgres:
	brew install postgresql@${POSTGRES_VERSION}
else ifeq ($(UNAME), Linux)
install-postgres:
	sudo apt-get update
	sudo apt-get install -y ca-certificates curl gnupg lsb-release
	curl https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor | sudo tee /etc/apt/trusted.gpg.d/apt.postgresql.org.gpg >/dev/null
	echo "deb [signed-by=/etc/apt/trusted.gpg.d/apt.postgresql.org.gpg] http://apt.postgresql.org/pub/repos/apt $$(lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list
	sudo apt-get update
	sudo apt-get install -y postgresql-${POSTGRES_VERSION}
else
install-postgres:
	echo "Unsupported platform: ${UNAME}"
	exit 1
endif

ifeq (${UNAME}, Darwin)
start-postgres:
	brew services start postgresql@${POSTGRES_VERSION}
else ifeq ($(UNAME), Linux)
start-postgres:
	sudo systemctl start postgresql
endif

ifeq (${UNAME}, Darwin)
stop-postgres:
	brew services stop postgresql@${POSTGRES_VERSION}
else ifeq ($(UNAME), Linux)
stop-postgres:
	sudo systemctl stop postgresql
endif

POSTGRES_DATABASE_NAME ?= eval_hub
POSTGRES_USER ?= eval_hub
POSTGRES_URL ?= postgres://${POSTGRES_USER}@localhost:5432/${POSTGRES_DATABASE_NAME}

create-database: ; $(info POSTGRES_URL = ${POSTGRES_URL})
	createdb-${POSTGRES_VERSION} ${POSTGRES_DATABASE_NAME}

delete-database: ; $(info POSTGRES_URL = ${POSTGRES_URL})
	dropdb-${POSTGRES_VERSION} ${POSTGRES_DATABASE_NAME}

create-user: ; $(info POSTGRES_URL = ${POSTGRES_URL})
	createuser-${POSTGRES_VERSION} -s -d -r ${POSTGRES_USER}

grant-permissions:
	psql-${POSTGRES_VERSION} --dbname=${POSTGRES_DATABASE_NAME} -c "GRANT ALL ON ALL TABLES IN SCHEMA public TO ${POSTGRES_USER};"

### Run postgres in a container

PODMAN ?= podman
POSTGRES_CONTAINER_NAME ?= postgres-${POSTGRES_VERSION}
POSTGRES_PASSWORD ?= eval-hub-test-password
POSTGRES_READY_RETRIES ?= 30

start-postgres-container:
	${PODMAN} run -d --name ${POSTGRES_CONTAINER_NAME} -p 127.0.0.1:5432:5432 -e POSTGRES_USER=${POSTGRES_USER} -e POSTGRES_PASSWORD=${POSTGRES_PASSWORD} -e POSTGRES_DB=${POSTGRES_DATABASE_NAME} postgres:${POSTGRES_VERSION}

cleanup-postgres-container:
	@${PODMAN} rm -f ${POSTGRES_CONTAINER_NAME} >/dev/null 2>&1 || true

wait-postgres-container:
	@for attempt in $$(seq 1 ${POSTGRES_READY_RETRIES}); do \
		if ${PODMAN} exec ${POSTGRES_CONTAINER_NAME} pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DATABASE_NAME}; then exit 0; fi; \
		sleep 1; \
	done; \
	echo "PostgreSQL container did not become ready"; \
	exit 1

stop-postgres-container:
	${PODMAN} stop ${POSTGRES_CONTAINER_NAME}

delete-postgres-container:
	${PODMAN} rm ${POSTGRES_CONTAINER_NAME}
