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

create-database:
	createdb-${POSTGRES_VERSION} eval_hub

delete-database:
	dropdb-${POSTGRES_VERSION} eval_hub

create-user:
	createuser-${POSTGRES_VERSION} -s -d -r eval_hub

grant-permissions:
	psql-${POSTGRES_VERSION} -c "GRANT ALL PRIVILEGES ON DATABASE eval_hub TO eval_hub;"
