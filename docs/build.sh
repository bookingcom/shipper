#!/bin/bash

pip install -r requirements.txt
sphinx-build -W --keep-going -b html . generated