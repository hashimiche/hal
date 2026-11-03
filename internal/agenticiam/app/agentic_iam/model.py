"""The model seam: the one place that decides which model drives the demo agent."""

from pydantic_ai.models import Model

from .fake_model import keyword_model


def chat_model() -> Model:
    """The demo agent's model.

    Any PydanticAI model that supports tool calling fits here. Swapping in a
    real one is this line, e.g. ``infer_model("anthropic:<model>")``, plus the
    ``pydantic-ai-slim[<provider>]`` extra in requirements.in. The deterministic
    fake keeps the six cases of the lab reproducible (ADR 0004, decision 9).
    """
    return keyword_model()
