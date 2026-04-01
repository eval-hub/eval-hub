.PHONY: install-postgres start-postgres stop-postgres create-database create-user grant-permissions start-postgres-container stop-postgres-container delete-postgres-container

UNAME := $(shell uname)

POSTGRES_VERSION ?= 18

ifeq (${UNAME}, Darwin)
install-postgres:
	brew install postgresql@${POSTGRES_VERSION}
else ifeq ($(UNAME), Linux)
install-postgres:
	sudo apt-get install postgresql-${POSTGRES_VERSION}
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

start-postgres-container:
	${PODMAN} run -d --name postgres-${POSTGRES_VERSION} -p 5432:5432 -e POSTGRES_USER=${POSTGRES_USER} -e POSTGRES_PASSWORD=${POSTGRES_PASSWORD} -e POSTGRES_DB=${POSTGRES_DATABASE_NAME} postgres:${POSTGRES_VERSION}

stop-postgres-container:
	${PODMAN} stop postgres-${POSTGRES_VERSION}

delete-postgres-container:
	${PODMAN} rm postgres-${POSTGRES_VERSION}
