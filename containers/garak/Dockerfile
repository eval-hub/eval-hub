FROM registry.access.redhat.com/ubi9/python-312:latest

# Set working directory
WORKDIR /app

# Install Python dependencies
COPY requirements.txt .
## installing cpu torch to reduce image size
RUN pip install --extra-index-url https://download.pytorch.org/whl/cpu torch
RUN pip install --no-cache-dir -r requirements.txt

# Copy component code (only what's needed)
COPY garak_component.py /app/
COPY result_parser.py /app/
COPY avid_report.py /app/

# Set XDG environment variables to use /tmp (always writable) for garak to write to
ENV XDG_CACHE_HOME=/tmp/.cache
ENV XDG_DATA_HOME=/tmp/.local/share
ENV XDG_CONFIG_HOME=/tmp/.config

# Set entrypoint
ENTRYPOINT ["python", "/app/garak_component.py"]
