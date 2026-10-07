"""The demo agent's tools: one per lab table, each covered by one scope.

The model sees the tools' names, descriptions and arguments. What a tool does
is a ``Runner`` supplied for the task, which holds the task's OBO token: the
token never reaches the model.
"""

from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from langchain_core.tools import BaseTool, StructuredTool


@dataclass(frozen=True)
class ToolSpec:
    name: str
    scope: str  # the scope, and so the task scope entry, that covers this tool
    table: str
    query: str


QUARTERLY_RESULTS = ToolSpec(
    name="get_quarterly_results",
    scope="vault:finance-reports",
    table="quarterly_results",
    query="SELECT quarter, revenue_musd, operating_margin_pct, commentary"
          " FROM quarterly_results WHERE quarter = %(quarter)s",
)
FORECASTS = ToolSpec(
    name="get_forecasts",
    scope="vault:forecasts",
    table="forecasts",
    query="SELECT quarter, revenue_musd, confidence FROM forecasts ORDER BY quarter",
)
PAYROLL = ToolSpec(
    name="get_payroll",
    scope="vault:payroll",
    table="payroll",
    query="SELECT employee, department, annual_salary_kusd FROM payroll ORDER BY employee",
)

SPECS = {spec.name: spec for spec in (QUARTERLY_RESULTS, FORECASTS, PAYROLL)}
SCOPES = [spec.scope for spec in SPECS.values()]

Runner = Callable[[ToolSpec, dict[str, Any]], str]


def make_tools(run: Runner) -> list[BaseTool]:
    def get_quarterly_results(quarter: str) -> str:
        """Read Acme's financial results for one quarter, such as "Q3"."""
        return run(QUARTERLY_RESULTS, {"quarter": quarter.strip().upper()})

    def get_forecasts() -> str:
        """Read Acme's revenue forecasts."""
        return run(FORECASTS, {})

    def get_payroll() -> str:
        """Read Acme's payroll."""
        return run(PAYROLL, {})

    return [StructuredTool.from_function(f) for f in (get_quarterly_results, get_forecasts, get_payroll)]


def planning_only(spec: ToolSpec, args: dict[str, Any]) -> str:
    """The Runner used while planning: deriving the task scope reads no data."""
    raise RuntimeError(f"{spec.name} called while planning")
