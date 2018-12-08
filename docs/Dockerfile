FROM python:3

RUN echo $HTTP_PROXY $HTTPS_PROXY
WORKDIR /usr/src/app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
CMD [ "sphinx-autobuild", "--host", "0.0.0.0", "-W", "-b", "html", ".", "generated" ]
