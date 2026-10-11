"""Smoke test the seeded development stack through its public HTTP endpoints."""
import json
import uuid
from urllib.error import HTTPError
from urllib.request import Request, urlopen

BASE = 'http://127.0.0.1:8080/api/v1'

def api(path, body=None, status=200):
    raw = None if body is None else json.dumps(body).encode()
    request = Request(BASE + path, data=raw, headers={'Content-Type': 'application/json'})
    try:
        response = urlopen(request, timeout=15)
    except HTTPError as error:
        assert error.code == status, (path, error.code, error.read())
        return json.load(error)
    assert response.status == status, (path, response.status)
    return json.load(response)

def rid():
    return str(uuid.uuid4())

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
    assert response.status == 200 and 'ELS' in response.read().decode()
print('Complete stack smoke test passed: scheduling, feedback, retry, undo, all three question types, Next.js.')
