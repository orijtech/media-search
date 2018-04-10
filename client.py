#!/usr/bin/env python

"""
Copyright 2018, OpenCensus Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
u may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
"""

import requests
from opencensus.trace.exporters import stackdriver_exporter
from opencensus.trace import tracer as tracer_module
from opencensus.trace import config_integration

# Trace our HTTP requests module
integration = ['requests']
config_integration.trace_integrations(integration)
exporter = stackdriver_exporter.StackdriverExporter(project_id='census-demos')
tracer = tracer_module.Tracer(exporter=exporter)

def main():
  while True:
    query = input('Content to search$ ')
    doSearch(query)

def doSearch(query):
  with tracer.span(name='py-search') as span:
    res = requests.post('http://localhost:9778/search', json={'q': query})
    pages = res.json()
    for page in pages:
      items = page['Items']
      for i, item in enumerate(items):
        id = item['id']
        if 'videoId' in id:
          print('URL: https://youtu.be/{videoId}'.format(**item['id']))
        elif 'channelId' in id:
          print('ChannelURL: https://www.youtube.com/channel/{channelId}'.format(**item['id']))

        snippet = item['snippet']
        snippet.setdefault('description', 'Unknown')
        print('Title: {title}\nDescription: {description}\n\n'.format(**snippet))

if __name__ == '__main__':
  main()
