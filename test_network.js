// Simple script to verify network connectivity
const https = require('https');

console.log('Testing access to cdn.playwright.dev...');

const url = 'https://cdn.playwright.dev/';
const req = https.get(url, (res) => {
  console.log(`Status Code: ${res.statusCode}`);
  let data = '';

  res.on('data', (chunk) => {
    data += chunk;
  });

  res.on('end', () => {
    console.log('Response received!');
    console.log(`First 100 characters of response: ${data.substring(0, 100)}`);
    console.log('Network connection successful!');
  });
});

req.on('error', (error) => {
  console.error('Error:', error.message);
});

req.end();