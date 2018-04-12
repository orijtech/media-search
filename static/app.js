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
	nodes.resultsSection.classList.add('results-section-expanded');
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

	if (result.resultType === 'video') {
		anchor.addEventListener('click', function(e) {
			e.preventDefault();

			showInlinePlayer(result.id);
		});
	}
}

function successCallback(response) {
	onSearchEnd();

	var results = response[0].Items.map(function(item) {
		var url = 'https://youtube.com/';
		var resultType = null;
		var thumbnail = null;
		var id = null;

		if (item.id.videoId) {
			id = item.id.videoId;
			resultType = 'video';
			url = url + 'watch?v=' + item.id.videoId;
		} else {
			id = item.id.channelId;
			resultType = 'channel';
			url = url + 'channel/' + item.id.channelId;
		}

		if (item.snippet.thumbnails.high) {
			thumbnail = item.snippet.thumbnails.high.url;
		} else if (item.snippet.thumbnail.medium) {
			thumbnail = item.snippet.thumbnails.medium.url;
		} else {
			item.snippet.thumbnails.default.url;
		}

		return {
			id: id,
			url: url,
			title: item.snippet.title,
			thumbnail: thumbnail,
			resultType: resultType
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
		data: {"q": query, "max_per_page": 20},
		url: 'http://localhost:9778/search',
		successCallback: successCallback,
		errorCallback: errorCallback
	});
}

function showInlinePlayer(youtubeId) {
	var background = document.createElement('div');
	var modal = document.createElement('div');
	var loader = document.createElement('i');
	var iframe = document.createElement('iframe');

	document.body.classList.add('modal-active');
	background.classList.add('modal-background', 'js-modal-background');
	modal.classList.add('modal');
	loader.classList.add('material-icons', 'loader', 'modal-loader');
	iframe.classList.add('modal-iframe');

	loader.textContent = 'cached';

	iframe.width = 560;
	iframe.height = 315;
	iframe.src = 'https://www.youtube.com/embed/' + youtubeId;
	iframe.frameborder = 0;
	iframe.allow = 'autoplay; encrypted-media';
	iframe.allowfullscreen = true;

	background.appendChild(modal);
	modal.appendChild(loader);
	modal.appendChild(iframe);
	document.body.appendChild(background);

	var scrollTop = window.pageYOffset || (document.documentElement || document.body.parentNode || document.body).scrollTop;
	background.style.top = scrollTop + 'px';

	background.addEventListener('click', function() {
		background.parentNode.removeChild(background);
		document.body.classList.remove('modal-active');
	});
}

nodes.searchButton.addEventListener('click', onSearchClick)
