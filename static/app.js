var nodes = {
	searchInput: document.querySelector('.js-search-input'),
	searchButton: document.querySelector('.js-search-button'),
	searchSection: document.querySelector('.js-search-section'),
	resultsSection: document.querySelector('.js-results-section'),
	loader: document.querySelector('.js-loader')
};

var isSearching = false;
var searchResults = [];

// Basic HTTP request method
function sendRequest(object) {
	var successCallback = object.successCallback,
		errorCallback = object.errorCallback,
		method = object.method.toUpperCase(),
		data = object.data,
		url = object.url,
		xhr = new XMLHttpRequest();

	var usesJson = (method === 'POST' || method === 'PUT');

	xhr.open(method, url);
	xhr.setRequestHeader('Access-Control-Allow-Origin', '*');

	if (usesJson) {
		xhr.setRequestHeader('Content-Type', 'application/json');
	}

	xhr.onreadystatechange = function() {
		if (xhr.readyState === 4) {
			if (xhr.status === 200) {
				return successCallback(JSON.parse(xhr.responseText));
			} else {
				return errorCallback(xhr.status);
			}
		}
	}

	if (usesJson) {
		xhr.send(JSON.stringify(data));
	} else {
		xhr.send();
	}
}

function collapseSearchSection() {
	if (nodes.searchSection.classList.contains('search-section-collapsed')) {
		return;
	}

	nodes.searchSection.classList.add('search-section-collapsed');
}

function clearResults() {
	var results = [].slice.call(document.querySelectorAll('.result-container'));
	results.forEach(function(node) {
		node.parentNode.removeChild(node);
	});
}

function onSearchBegin() {
	collapseSearchSection();
	clearResults();
	nodes.loader.classList.remove('hidden');
	isSearching = true;
}

function onSearchEnd() {
	isSearching = false;
	nodes.loader.classList.add('hidden');
}

function appendResult(result) {
	var div = document.createElement('div');
	var anchor = document.createElement('a');
	var imgDiv = document.createElement('div');
	var img = document.createElement('img');
	var p = document.createElement('p');

	div.classList.add('result-container');
	anchor.classList.add('result-link');
	imgDiv.classList.add('result-image-container');
	p.classList.add('result-title');

	anchor.href = result.url;
	imgDiv.style.backgroundImage = 'url('+result.thumbnail+')';
	imgDiv.style.backgroundPosition = 'center';
	imgDiv.style.backgroundSize = 'cover';
	p.textContent = result.title;

	div.appendChild(anchor);
	div.appendChild(imgDiv);
	div.appendChild(p);
	nodes.resultsSection.appendChild(div);
}

function successCallback(response) {
	onSearchEnd();

	var results = response[0].Items.map(function(item) {
		var url = 'https://youtube.com/';
		if (item.id.videoId) {
			url = url + 'watch?v=' + item.id.videoId;
		} else {
			url = url + 'channel/' + item.id.channelId;
		}

		return {
			url: url,
			title: item.snippet.title,
			thumbnail: item.snippet.thumbnails.default.url
		};
	});

	results.forEach(function(result) {
		appendResult(result);
	});
}

function errorCallback() {
	onSearchEnd();
	alert('Something went wrong.');
}

function onSearchClick() {
	if (isSearching) {
		return;
	}

	onSearchBegin();

	var query = nodes.searchInput.value.trim();

	sendRequest({
		method: 'POST',
		data: {"q": query},
		url: 'http://localhost:9778/search',
		successCallback: successCallback,
		errorCallback: errorCallback
	});
}

nodes.searchButton.addEventListener('click', onSearchClick)
