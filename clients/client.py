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

def main():
  while True:
    query = input('Content to search$ ')
    doSearch(query)

def doSearch(query):
    res = requests.post('http://localhost:9779/search', json={'keywords': query})
    print(res.text, res.status_code)
    pages = res.json()
    for page in pages:
      items = page.get('items', [])
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
