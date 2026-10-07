# One image for both containers of the Agentic IAM lab: `chat` and `agent`
# (ADR 0004, decision 10). HAL builds it locally from the sources embedded in
# its binary and tags it with their hash.
#
# The base is pinned by tag and by multi-arch index digest, and every Python
# package is installed from a lock file that carries its hashes.
FROM docker.io/library/python:3.14.8-slim-trixie@sha256:f85c5697265c178cc6887276c55fe16cf3d14ca35c3df6a5eab3b360534a55d2

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

COPY requirements.txt /tmp/requirements.txt
RUN pip install --root-user-action=ignore --require-hashes --no-deps --only-binary=:all: \
        -r /tmp/requirements.txt \
    && rm /tmp/requirements.txt

RUN useradd --uid 10001 --user-group --no-create-home --shell /usr/sbin/nologin hal

WORKDIR /app
COPY agentic_iam /app/agentic_iam

USER 10001
ENTRYPOINT ["python", "-m", "agentic_iam"]
CMD ["--help"]
