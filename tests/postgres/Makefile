.PHONY: install-postgres start-postgres stop-postgres create-database create-user grant-permissions

UNAME := $(shell uname)

POSTGRES_VERSION ?= 18

ifeq (${UNAME}, Darwin)
install-postgres:
	brew install postgresql@${POSTGRES_VERSION}
else ifeq ($(UNAME), Linux)
install-postgres:
	sudo apt-get install postgresql
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

create-database:
	createdb-${POSTGRES_VERSION} ${POSTGRES_DATABASE_NAME}

delete-database:
	dropdb-${POSTGRES_VERSION} ${POSTGRES_DATABASE_NAME}

create-user:
	createuser-${POSTGRES_VERSION} -s -d -r ${POSTGRES_USER}

grant-permissions:
	psql-${POSTGRES_VERSION} --dbname=${POSTGRES_DATABASE_NAME} -c "GRANT ALL ON ALL TABLES IN SCHEMA "public" TO ${POSTGRES_USER};"
