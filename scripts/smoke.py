"""Smoke test the seeded development stack through its public HTTP endpoints."""
import json
import os
from http.cookiejar import CookieJar
import uuid
from urllib.error import HTTPError
from urllib.request import Request, urlopen, build_opener, HTTPCookieProcessor

BASE = 'http://127.0.0.1:8080/api/v1'
opener = build_opener(HTTPCookieProcessor(CookieJar()))
csrf = ''

def api(path, body=None, status=200):
    raw = None if body is None else json.dumps(body).encode()
    request = Request(BASE + path, data=raw, headers={'Content-Type': 'application/json', 'X-ELS-Client': 'web', 'X-CSRF-Token': csrf})
    try:
        response = opener.open(request, timeout=15)
    except HTTPError as error:
        assert error.code == status, (path, error.code, error.read())
        return json.load(error)
    assert response.status == status, (path, response.status)
    return json.load(response)

def rid():
    return str(uuid.uuid4())

api('/auth/login', {'username': os.environ.get('ELS_ADMIN_USERNAME', 'admin'), 'password': os.environ['ELS_ADMIN_PASSWORD'], 'code': ''})
state = api('/auth/me')
csrf = state['csrf_token']
if state['user']['must_change_password']:
    api('/auth/password', {'current_password': os.environ['ELS_ADMIN_PASSWORD'], 'new_password': os.environ['ELS_ADMIN_PASSWORD'] + '-changed'})
    api('/auth/login', {'username': os.environ.get('ELS_ADMIN_USERNAME', 'admin'), 'password': os.environ['ELS_ADMIN_PASSWORD'] + '-changed', 'code': ''})
    csrf = api('/auth/me')['csrf_token']
spellings = {'适应': 'adapt', '采用': 'adopt', '维持': 'maintain', '后果': 'consequence', '必要的': 'necessary'}
meanings = {value: key for key, value in spellings.items()}
created = api('/study/sessions', {'types': ['spelling'], 'limit': 2, 'new_limit': 2}, 201)
sid = created['id']
path = f'/study/sessions/{sid}'
session = api(path)
assert len(session['questions']) == 2
assert all('expected' not in q for q in session['questions'])
assert api('/study/sessions/active')['session']['id'] == sid
for q in session['questions']:
    answer = {'question_id': q['id'], 'request_id': rid(), 'answer': spellings[q['prompt']], 'response_time_ms': 1200, 'used_hint': False}
    grade = api(path + '/answers', answer)
    assert grade['result'] == 'correct'
    assert api(path + '/answers', answer)['replayed']
    restored = next(item for item in api(path)['questions'] if item['id'] == q['id'])
    assert restored['rating'] is None and restored['expected'] == answer['answer']
    review = {'question_id': q['id'], 'request_id': rid(), 'rating': 3}
    rated = api(path + '/review', review)
    assert rated['schedule_applied'] and rated['next_review_at']
    assert api(path + '/review', review)['replayed']
assert api('/study/sessions/active')['session'] is None
undo = {'request_id': rid()}
api(path + '/undo', undo)
assert api(path + '/undo', undo)['replayed']
q = api(path)['questions'][-1]
assert q['result'] is None
api(path + '/answers', {'question_id': q['id'], 'request_id': rid(), 'answer': spellings[q['prompt']], 'response_time_ms': 900, 'used_hint': True})
api(path + '/review', {'question_id': q['id'], 'request_id': rid(), 'rating': 3}, 400)
api(path + '/review', {'question_id': q['id'], 'request_id': rid(), 'rating': 1})
for kind in ['recognition', 'meaning_recall']:
    sid = api('/study/sessions', {'mode': 'practice', 'types': [kind], 'limit': 1}, 201)['id']
    path = f'/study/sessions/{sid}'
    q = api(path)['questions'][0]
    grade = api(path + '/answers', {'question_id': q['id'], 'request_id': rid(), 'answer': meanings[q['prompt']], 'response_time_ms': 900, 'used_hint': False})
    assert grade['result'] == 'correct'
    assert not api(path + '/review', {'question_id': q['id'], 'request_id': rid(), 'rating': 3})['schedule_applied']
dashboard = api('/dashboard')
assert dashboard['scheduler'] == 'FSRS-6'
assert dashboard['today']['reviewed'] == 4
assert api('/study/sessions/active')['session'] is None
with urlopen('http://127.0.0.1:3000/', timeout=15) as response:
    assert response.status == 200 and 'Learning' in response.read().decode()
assert api('/learning/catalog')['region'] == '广东'
assert len(api('/learning/catalog')['subjects']) == 6
print('Complete stack smoke test passed: scheduling, feedback, retry, undo, authenticated web, scheduling, feedback, retry, undo, three text question types, Next.js.')

# Exercise the real API/Redis/Worker path without external AI credentials.
from pathlib import Path
import time
pdf = (Path(__file__).resolve().parent.parent / 'services/api/cmd/api/testdata/questions.pdf').read_bytes()
boundary = 'LearningSmoke' + uuid.uuid4().hex
multipart = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="smoke.pdf"\r\nContent-Type: application/pdf\r\n\r\n'.encode() + pdf + f'\r\n--{boundary}--\r\n'.encode())
def upload_original():
    req = Request(BASE + '/admin/resources/uploads', data=multipart, headers={'Content-Type': 'multipart/form-data; boundary=' + boundary, 'X-ELS-Client': 'web', 'X-CSRF-Token': csrf})
    with opener.open(req, timeout=20) as response:
        assert response.status == 201
        return json.load(response)['id']
resource = upload_original()
assert upload_original() == resource, 'same original created a duplicate resource'
job = api('/admin/resources/' + resource + '/classify', {'revision': 1}, 202)['job_id']
assert api('/admin/resources/' + resource + '/classify', {'revision': 1}, 202)['job_id'] == job
for _ in range(40):
    status = api('/admin/resources/jobs/' + job + '/status')['job']
    if status['status'] in ['completed', 'failed']:
        break
    time.sleep(.5)
assert status['status'] == 'completed', status
listed = api('/admin/resources?q=smoke.pdf')['resources']
assert len(listed) == 1 and listed[0]['classification_status'] == 'needs_confirmation'
assert api('/admin/runtime')['worker_online'], 'separate worker heartbeat missing'
with opener.open('http://127.0.0.1:3000/api/v1/admin/resources/' + resource + '/file', timeout=20) as response:
    assert response.read() == pdf, 'worker changed original PDF'
    assert "frame-ancestors 'self'" in response.headers['Content-Security-Policy'], 'same-site PDF comparison blocked'
    assert response.headers['Content-Type'].startswith('application/pdf')
print('Learning v2 resource smoke passed: upload dedupe, queue idempotency, separate worker, uncertainty, immutable original and PDF comparison headers; no paid AI calls.')
