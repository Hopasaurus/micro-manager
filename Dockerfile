# syntax=docker/dockerfile:1
#
# Development container for the micro-manager repository.
#
#   docker build -t micro-manager-dev .
#   docker run -it --rm -v "$PWD":/workspace micro-manager-dev
#
# Contains everything the project needs:
#   - Go 1.26.5 (+ gopls)            implementations/golang
#   - Node.js LTS-ish + npm + npx    implementations/typescript, pi
#   - Neovim                          editor (VISUAL/EDITOR for mm --detail)
#   - Python 3                        implementations/python/mmx
#   - Erlang/OTP (BEAM) + rebar3     implementations/erlang
#     (erlang-nox: the full OTP set minus the wx/observer GUI stack, which
#      cannot display in a container and drags in webkit2gtk)
#   - pi coding agent                 @earendil-works/pi-coding-agent
#   - git, ripgrep, jq, build tools   check.sh, find.sh, general dev

FROM debian:trixie-slim

# Built-in platform args are only in scope after being re-declared in the
# stage; without this line ${TARGETARCH} expands empty on arm64 hosts and the
# fallback silently downloads the amd64 Go toolchain.
ARG TARGETARCH

ARG GO_VERSION=1.26.5
ARG NODE_VERSION=26.5.0
ARG GOPLS_VERSION=latest
ARG USERNAME=dev
ARG USER_UID=1000
ARG USER_GID=1000

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    EDITOR=nvim \
    VISUAL=nvim

# --- System packages --------------------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        bash-completion \
        build-essential \
        ca-certificates \
        curl \
        git \
        jq \
        less \
        neovim \
        procps \
        python3 \
        python3-pip \
        python3-venv \
        ripgrep \
        sudo \
        unzip \
        xz-utils \
        erlang-nox \
        erlang-dialyzer \
        rebar3 \
    && rm -rf /var/lib/apt/lists/*

# --- Go ----------------------------------------------------------------------
# Verified against the .sha256 file published on dl.google.com (the CDN go.dev
# redirects to; go.dev itself serves an HTML page for the .sha256 URL), so no
# hash is hard-coded here. TARGETARCH is amd64 or arm64; Go uses the same names.
RUN arch="${TARGETARCH:-amd64}" \
    && curl -fsSL "https://dl.google.com/go/go${GO_VERSION}.linux-${arch}.tar.gz" -o /tmp/go.tgz \
    && curl -fsSL "https://dl.google.com/go/go${GO_VERSION}.linux-${arch}.tar.gz.sha256" -o /tmp/go.sha256 \
    && echo "$(cat /tmp/go.sha256)  /tmp/go.tgz" | sha256sum -c - \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz /tmp/go.sha256

ENV PATH=/usr/local/go/bin:$PATH

# gopls: the repository's working rules expect a Go language server.
RUN GOBIN=/usr/local/bin go install "golang.org/x/tools/gopls@${GOPLS_VERSION}" \
    && rm -rf /root/go /root/.cache

# --- Node.js ------------------------------------------------------------------
# npm and npx ship with Node. Verified against SHASUMS256.txt from nodejs.org.
# Node calls amd64 "x64"; arm64 matches TARGETARCH.
RUN arch="${TARGETARCH:-amd64}" \
    && case "$arch" in \
         amd64) node_arch=x64 ;; \
         arm64) node_arch=arm64 ;; \
         *) echo "unsupported arch: $arch" >&2; exit 1 ;; \
       esac \
    && cd /tmp \
    && curl -fsSLO "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${node_arch}.tar.xz" \
    && curl -fsSLO "https://nodejs.org/dist/v${NODE_VERSION}/SHASUMS256.txt" \
    && grep -F "node-v${NODE_VERSION}-linux-${node_arch}.tar.xz" SHASUMS256.txt | sha256sum -c - \
    && tar -C /usr/local --strip-components=1 -xJf "node-v${NODE_VERSION}-linux-${node_arch}.tar.xz" \
    && rm "node-v${NODE_VERSION}-linux-${node_arch}.tar.xz" SHASUMS256.txt \
    && npm cache clean --force

# --- pi coding agent -----------------------------------------------------------
RUN npm install -g --ignore-scripts @earendil-works/pi-coding-agent \
    && npm cache clean --force

# --- Non-root developer ---------------------------------------------------------
RUN groupadd --gid "${USER_GID}" "${USERNAME}" \
    && useradd --uid "${USER_UID}" --gid "${USER_GID}" --create-home --shell /bin/bash "${USERNAME}" \
    && echo "${USERNAME} ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/${USERNAME}" \
    && chmod 0440 "/etc/sudoers.d/${USERNAME}"

# npm -g for the dev user lands in the home directory; the root-installed pi
# stays on PATH via /usr/local/bin. GOPATH lives in the home directory too.
ENV NPM_CONFIG_PREFIX=/home/${USERNAME}/.npm-global \
    GOPATH=/home/${USERNAME}/go \
    PATH=/home/${USERNAME}/.npm-global/bin:/home/${USERNAME}/go/bin:/usr/local/go/bin:${PATH}

USER ${USERNAME}

# A mounted checkout may be owned by the host user; do not let git refuse it.
RUN git config --global --add safe.directory /workspace

WORKDIR /workspace

LABEL org.opencontainers.image.title="micro-manager-dev" \
      org.opencontainers.image.description="Go ${GO_VERSION}, Node.js ${NODE_VERSION}, Neovim, Python 3, Erlang/OTP, pi coding agent" \
      org.opencontainers.image.base.name="debian:trixie-slim"

CMD ["bash"]
