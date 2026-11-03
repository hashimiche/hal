"""MariaDB, reached with the ephemeral user that Vault issued for one tool call."""

from typing import Any

import pymysql
import pymysql.cursors

from .vault import Lease


class DatabaseError(Exception):
    """The lab database could not be queried."""


class Database:
    def __init__(self, host: str, port: int, name: str):
        self.host = host
        self.port = port
        self.name = name

    def select(self, lease: Lease, query: str, args: dict[str, Any]) -> list[dict[str, Any]]:
        try:
            connection = pymysql.connect(
                host=self.host,
                port=self.port,
                user=lease.username,
                password=lease.password,
                database=self.name,
                connect_timeout=5,
                read_timeout=10,
                cursorclass=pymysql.cursors.DictCursor,
            )
            with connection, connection.cursor() as cursor:
                cursor.execute(query, args)
                return list(cursor.fetchall())
        except pymysql.MySQLError as err:
            raise DatabaseError(str(err)) from None
