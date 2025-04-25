'''
MAIN CLIENT/LEAF script
'''

import sys
import threading
from flask import Flask, jsonify, request
from clientRequests import client_work

cli = sys.modules['flask.cli']
cli.show_server_banner = lambda *x: None

# Using flask for backend
app = Flask(__name__)

# Temperature data
temperature = [
    {"id": 1, "measure": "20C", "date": "Apr 4th 2025"},
    {"id": 2, "measure": "25C", "date": "Jan 3rd 2025"},
    {"id": 3, "measure": "18C", "date": "Feb 2nd 2025"},
]

# Get all temperatures
@app.route('/temperature', methods=['GET'])
def get_temperature():
    return jsonify(temperature)


if __name__ == '__main__':
    try:
        # Run client in a separate thread so that main thread concetrates on just providing its service
        t1 = threading.Thread(target=client_work)
        t1.start()

        # Main thread functionality to run Flask server
        app.run(debug=False)

    except KeyboardInterrupt as ki:
        print("Keyboard Interrupt performed. Exiting....")
        sys.exit(0)
    
    except Exception as e:
        print(f"Something went wrong - {e}")
        sys.exit(0)
