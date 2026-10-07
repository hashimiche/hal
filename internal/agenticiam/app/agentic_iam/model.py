"""The model seam: the one place that decides which chat model drives the demo agent."""

from langchain_core.language_models import BaseChatModel

from .fake_model import KeywordChatModel


def chat_model() -> BaseChatModel:
    """The demo agent's model.

    Any LangChain chat model that supports tool calling (``bind_tools``) fits
    here. Swapping in a real one is this line, plus its ``langchain-<provider>``
    package in requirements.in. The deterministic fake keeps the six cases of
    the lab reproducible (ADR 0004, decision 9).
    """
    return KeywordChatModel()
