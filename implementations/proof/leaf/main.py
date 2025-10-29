'''
MAIN CLIENT/LEAF script
'''

import sys
import signal
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

# Global flag to stop the thread
close_thread = False

def thread_run_in_bg():
    while not close_thread:
        client_work()

def signal_handler(signum, frame):
    print("Interrupt signal received, shutting down...")
    global close_thread
    close_thread = True
    sys.exit(0)


if __name__ == '__main__':
    try:
        signal.signal(signal.SIGINT, signal_handler)
        # Run client in a separate thread so that main thread concetrates on just providing its service
        t1 = threading.Thread(target=thread_run_in_bg)
        t1.daemon = True  # So it doesn't block the exit
        t1.start()        

        close_thread = True

        # Main thread functionality to run Flask server
        app.run(debug=False, port=1299)

    except KeyboardInterrupt as ki:
        print("Keyboard Interrupt performed. Exiting....")
        close_thread = True
        sys.exit(0)
    
    except Exception as e:
        print(f"Something went wrong - {e}")
        close_thread = True
        sys.exit(0)
